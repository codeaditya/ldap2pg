package privilege

import (
	"strings"

	"github.com/dalibo/ldap2pg/internal/postgres"
	"github.com/jackc/pgx/v5"
	"golang.org/x/exp/slog"
)

// Grant holds privilege informations from Postgres inspection or Grant rule.
//
// Not to confuse with Privilege. A Grant references an object, a role and a
// privilege via the Target field. It's somewhat like aclitem object in
// PostgreSQL.
//
// When Owner is non-zero, the grant represent a default privilege grant. The
// meansing of Object field change to hold the privilege class : TABLES,
// SEQUENCES, etc. instead of the name of an object.
type Grant struct {
	Target   string // Name of the referenced privilege object: DATABASE, TABLES, etc.
	Owner    string // For default privilege. Empty otherwise.
	Grantee  string
	Type     string // Privilege type (USAGE, SELECT, etc.)
	Database string // "" for instance grant.
	Schema   string // "" for database grant.
	Object   string // "" for both schema and database grants.
	Partial  bool   // Used for ALL TABLES permissions.
}

func RowTo(row pgx.CollectableRow) (g Grant, err error) {
	err = row.Scan(&g.Owner, &g.Grantee, &g.Type, &g.Database, &g.Schema, &g.Object, &g.Partial)
	return
}

func (g Grant) IsDefault() bool {
	return "" != g.Owner
}

// Expand wanted grants.
func (g Grant) Expand(databases postgres.DBMap) (out []Grant) {
	p := g.Privilege()
	for _, expansion := range p.Expand(g, databases) {
		slog.Debug("Expand grant.", "grant", expansion)
		out = append(out, expansion)
	}
	return
}

// Normalize ensures grant fields are consistent with privilege scope.
//
// This way grants from wanted state and from inspect are comparables.
func (g *Grant) Normalize() {
	if g.IsDefault() {
		g.Object = ""
		// Default grant rule schema is __all__. But default privilege
		// on all schemas is handled globally at database scope. Just
		// handle all schema as a single database privilege. Prevent
		// expanding the grant on all schema.
		if "__all__" == g.Schema {
			g.Schema = ""
		}
		return
	}

	p := g.Privilege()
	switch p.Scope {
	case "instance":
		// Allow to use Database as object name for database.
		if "" == g.Object {
			g.Object = g.Database
		}

		g.Database = ""
		g.Schema = ""
	case "database":
		if "" == g.Object {
			g.Object = g.Schema
		}
		g.Schema = ""
	default:
		slog.Debug("Normalizing grant.", "scope", p.Scope, "grant", g)
		panic("unhandled privilege scope")
	}
}

func (g Grant) Privilege() (p Privilege) {
	if !g.IsDefault() {
		p = Map[g.Target]
	} else if "" == g.Schema {
		p = Map["GLOBAL DEFAULT"]
	}
	if p.IsZero() {
		slog.Debug("Resolving privilege for grant.", "grant", g)
		panic("unhandled privilege")
	}
	return
}

func (g Grant) String() string {
	b := strings.Builder{}
	if g.Partial {
		b.WriteString("PARTIAL ")
	}
	if g.IsDefault() {
		b.WriteString("DEFAULT FOR ")
		b.WriteString(g.Owner)
		b.WriteByte(' ')
	}
	b.WriteString(g.Type)
	b.WriteString(" ON ")
	b.WriteString(g.Target)
	if !g.IsDefault() {
		b.WriteByte(' ')
		o := strings.Builder{}
		o.WriteString(g.Database)
		if "" != g.Schema {
			if o.Len() > 0 {
				o.WriteByte('.')
			}
			o.WriteString(g.Schema)
		}
		if "" != g.Object {
			if o.Len() > 0 {
				o.WriteByte('.')
			}
			o.WriteString(g.Object)
		}
		b.WriteString(o.String())
	} else if "" != g.Schema {
		b.WriteString(" IN SCHEMA ")
		b.WriteString(g.Schema)
	}

	if "" != g.Grantee {
		b.WriteString(" TO ")
		b.WriteString(g.Grantee)
	}

	return b.String()
}
