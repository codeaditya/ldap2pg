package privileges

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/dalibo/ldap2pg/v6/internal/postgres"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/jackc/pgx/v5"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

// Grant holds privilege informations from Postgres inspection or Grant rule.
//
// Not to confuse with Privilege. A Grant references an object, a role and a
// privilege via the Type field. It's somewhat like aclitem object in
// PostgreSQL.
//
// When Owner is non-zero, the grant represent a default privilege grant. The
// meaning of Object field changes to hold the privilege class : TABLES,
// SEQUENCES, etc. instead of the name of an object.
type Grant struct {
	Owner    string // For default privileges. Empty otherwise.
	Grantee  string
	ACL      string // Name of the referenced ACL: DATABASE, TABLES, etc.
	Type     string // Privilege type (USAGE, SELECT, etc.)
	Database string // "" for instance grant.
	Schema   string // "" for database grant.
	Object   string // "" for both schema and database grants.
	Partial  bool   // Used for ALL TABLES permissions.
}

func (g Grant) IsWildcard() bool {
	return g.Type != ""
}

var qArgRe = regexp.MustCompile(`<[a-z]+>`)

// FormatQuery replaces placeholders in query
//
// Replace keywords as-is, prepare arguments for postgres.SyncQuery.
//
// A placeholder is an identifier wrapped in angle brackets. We use a custom format
// to manage multi-stage formatting: keywords, then identifiers.
//
// Returns a SyncQuery with query and arguments.
// SyncQuery is zero if a placeholder can't be replaced.
func (g Grant) FormatQuery(s string) (q postgres.SyncQuery) {
	// Protect like patterns.
	s = strings.ReplaceAll(s, "%", "%%")

	// Replace keywords in query.
	s = strings.ReplaceAll(s, "<privilege>", g.Type)
	s = strings.ReplaceAll(s, "<acl>", g.ACL)
	if strings.Contains(s, "<owner>") {
		// default privileges are by design on keywords like TABLES, not identiers.
		s = strings.ReplaceAll(s, "<object>", g.Object)
	}

	var args []any
	for _, m := range qArgRe.FindAllString(s, -1) {
		s = strings.Replace(s, m, "%s", 1)
		switch m {
		case "<database>":
			args = append(args, pgx.Identifier{g.Database})
		case "<grantee>":
			args = append(args, pgx.Identifier{g.Grantee})
		case "<object>":
			args = append(args, pgx.Identifier{g.Object})
		case "<owner>":
			args = append(args, pgx.Identifier{g.Owner})
		case "<schema>":
			args = append(args, pgx.Identifier{g.Schema})
		default:
			return
		}
	}
	q.Query = s
	q.QueryArgs = args
	return
}

func (g Grant) String() string {
	b := strings.Builder{}
	if g.Partial {
		b.WriteString("PARTIAL ")
	}

	if g.Owner != "" {
		if g.Schema == "" {
			b.WriteString("GLOBAL ")
		}
		b.WriteString("DEFAULT FOR ")
		b.WriteString(g.Owner)
		if g.Schema != "" {
			b.WriteString(" IN SCHEMA ")
			b.WriteString(g.Schema)
		}
		b.WriteByte(' ')
	}

	if g.Type == "" {
		b.WriteString("ANY")
	} else {
		b.WriteString(g.Type)
	}
	b.WriteString(" ON ")
	if g.Owner != "" {
		b.WriteString(g.Object)
	} else {
		b.WriteString(g.ACL)
		b.WriteByte(' ')
		o := strings.Builder{}
		if g.Database != "" && g.Schema == "" && g.Object == "" {
			o.WriteString(g.Database)
		} else {
			o.WriteString(g.Schema)
			if g.Object != "" {
				if o.Len() > 0 {
					o.WriteByte('.')
				}
				o.WriteString(g.Object)
			}
		}
		b.WriteString(o.String())
	}

	if g.Grantee != "" {
		b.WriteString(" TO ")
		b.WriteString(g.Grantee)
	}

	return b.String()
}

func (g Grant) ExpandDatabase(database string) (out []Grant) {
	instanceWide := acls[g.ACL].Scope == "instance"

	if g.Database == "__all__" {
		// Needs to substitute to one or all (instance-wide acl) databases.
		for name := range postgres.Databases {
			if name != database && !instanceWide {
				// filter database-wide grant to current database only.
				continue
			}
			g := g // copy
			g.Database = name
			out = append(out, g)
		}
		if len(out) == 0 {
			panic(fmt.Sprintf("%s not in inspected databases", database))
		}
		return
	}

	if instanceWide || g.Database == database {
		// Accept grant on explicit database if instance-wide or for current database.
		// Since main synchronize instanceWide ACL on default database, we cant have
		// twice an explicit GRANT on default database and on target database. e.g. we
		// wont have GRANT CONNECT on extra0 executed on both postgres and extra0 database.
		out = append(out, g)
	}

	return
}

func (g Grant) ExpandOwners(database postgres.Database) (out []Grant) {
	if g.Owner != "__auto__" {
		out = append(out, g)
		return
	}

	if database.Name != g.Database {
		return
	}

	// Yield default privilege for database owner.
	var schemas []postgres.Schema
	if g.Schema == "" {
		schemas = maps.Values(database.Schemas)
	} else {
		schemas = []postgres.Schema{database.Schemas[g.Schema]}
	}

	creators := mapset.NewSet[string]()
	for _, s := range schemas {
		creators.Append(s.Creators...)
	}
	creatorsList := creators.ToSlice()
	slices.Sort(creatorsList)

	for _, role := range creatorsList {
		if role == g.Grantee {
			// Avoid granting on self.
			continue
		}
		g := g // copy
		g.Owner = role
		out = append(out, g)
	}

	return
}

func (g Grant) ExpandSchemas(schemas []string) (out []Grant) {
	if g.Schema != "__all__" {
		out = append(out, g)
		return
	}

	for _, name := range schemas {
		g := g // copy
		g.Schema = name
		out = append(out, g)
	}

	return
}

// Expand grants from rules.
//
// e.g.: instantiate a grant on all databases for each database.
// Same for schemas and owners.
func Expand(in []Grant, database postgres.Database) (out []Grant) {
	for _, grant := range in {
		out = append(out, grant.ExpandDatabase(database.Name)...)
	}

	in = out
	out = nil
	schemas := maps.Keys(database.Schemas)
	for _, grant := range in {
		out = append(out, grant.ExpandSchemas(schemas)...)
	}

	in = out
	out = nil
	for _, grant := range in {
		for _, expansion := range grant.ExpandOwners(database) {
			out = append(out, expansion)
			// Log full expansion.
			slog.Debug("Wants grant.", "grant", expansion, "database", grant.Database)
		}
	}

	return
}
