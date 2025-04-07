package role

import (
	"github.com/dalibo/ldap2pg/internal/postgres"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/jackc/pgx/v5"
	"github.com/mitchellh/mapstructure"
)

type Role struct {
	Name         string
	Comment      string
	Parents      []Membership
	Options      Options
	Config       *Config
	BeforeCreate string
	AfterCreate  string
}

func New() Role {
	r := Role{}
	r.Config = &Config{}
	return r
}

func RowTo(row pgx.CollectableRow) (r Role, err error) {
	var variableRow any
	var parents []any // jsonb
	var config []string
	r = New()
	err = row.Scan(&r.Name, &variableRow, &r.Comment, &parents, &config)
	if err != nil {
		return
	}
	for _, jsonb := range parents {
		if jsonb == nil {
			continue
		}
		var m Membership
		err = mapstructure.Decode(jsonb, &m)
		if err != nil {
			return r, err
		}
		r.Parents = append(r.Parents, m)
	}
	r.Options.LoadRow(variableRow.([]any))
	(*r.Config).Parse(config)
	return
}

func (r *Role) String() string {
	return r.Name
}

func (r *Role) BlacklistKey() string {
	return r.Name
}

// Generate queries to update current role configuration to match wanted role
// configuration.
func (r *Role) Alter(wanted Role) (out []postgres.SyncQuery) {
	identifier := pgx.Identifier{r.Name}

	optionsChanges := r.Options.Diff(wanted.Options)
	if optionsChanges != "" {
		out = append(out, postgres.SyncQuery{
			Description: "Alter options.",
			LogArgs: []any{
				"role", r.Name,
				"options", optionsChanges,
			},
			Query:     `ALTER ROLE %s WITH ` + optionsChanges + `;`,
			QueryArgs: []any{identifier},
		})
	}

	missingMemberships := r.MissingParents(wanted.Parents)
	if len(missingMemberships) > 0 {
		var parentIdentifiers []any
		for _, membership := range missingMemberships {
			parentIdentifiers = append(parentIdentifiers, pgx.Identifier{membership.Name})
		}
		out = append(out, postgres.SyncQuery{
			Description: "Grant missing parents.",
			LogArgs: []any{
				"role", r.Name,
				"parents", missingMemberships,
			},
			Query:     `GRANT %s TO %s;`,
			QueryArgs: []any{parentIdentifiers, identifier},
		})
	}
	spuriousMemberships := wanted.MissingParents(r.Parents)
	for _, membership := range spuriousMemberships {
		out = append(out, postgres.SyncQuery{
			Description: "Revoke spurious parent.",
			LogArgs: []any{
				"role", r.Name,
				"parent", membership.Name,
				"grantor", membership.Grantor,
			},
			// When running unprivileged, managing role must have admin option
			// on parent but also on grantor of membership. Otherwise, Postgres
			// raises a warning. To force Postgres to raise an error, set
			// explicitly the grantor in GRANTED BY clause.
			//
			// This has been discussed a lot for Postgres 16 on pgsql-hackers:
			// - https://www.postgresql.org/message-id/flat/CAAvxfHdB%3D0vnwbNbNC%2BdrEWUhpM6efHm8%3D%2BjRYCpc%3DnY5FHXew%40mail.gmail.com#43a711d60b82986e417b2f1a3233ad19
			// - https://www.postgresql.org/message-id/9c45a5a19718388678d11e0b48b400ad7e3e3d21.camel@dalibo.com
			Query:     `REVOKE %s FROM %s GRANTED BY %s;`,
			QueryArgs: []any{pgx.Identifier{membership.Name}, identifier, pgx.Identifier{membership.Grantor}},
		})
	}

	if wanted.Comment != r.Comment {
		out = append(out, postgres.SyncQuery{
			Description: "Set role comment.",
			LogArgs: []any{
				"role", r.Name,
				"current", r.Comment,
				"wanted", wanted.Comment,
			},
			Query:     `COMMENT ON ROLE %s IS %s;`,
			QueryArgs: []any{identifier, wanted.Comment},
		})
	}

	if wanted.Config != nil {
		currentKeys := mapset.NewSetFromMapKeys(*r.Config)
		wantedKeys := mapset.NewSetFromMapKeys(*wanted.Config)
		missingKeys := wantedKeys.Clone()
		for k := range currentKeys.Iter() {
			if !wantedKeys.Contains(k) {
				out = append(out, postgres.SyncQuery{
					Description: "Reset role config.",
					LogArgs: []any{
						"role", r.Name,
						"config", k,
					},
					Query:     `ALTER ROLE %s RESET %s;`,
					QueryArgs: []any{identifier, pgx.Identifier{k}},
				})
				continue
			}

			missingKeys.Remove(k)

			currentValue := (*r.Config)[k]
			wantedValue := (*wanted.Config)[k]
			if wantedValue == currentValue {
				continue
			}
			out = append(out, postgres.SyncQuery{
				Description: "Update role config.",
				LogArgs: []any{
					"role", r.Name,
					"config", k,
					"current", currentValue,
					"wanted", wantedValue,
				},
				Query:     `ALTER ROLE %s SET %s TO %s;`,
				QueryArgs: []any{identifier, pgx.Identifier{k}, wantedValue},
			})
		}

		for k := range missingKeys.Iter() {
			v := (*wanted.Config)[k]
			out = append(out, postgres.SyncQuery{
				Description: "Set role config.",
				LogArgs: []any{
					"role", r.Name,
					"config", k,
					"value", v,
				},
				Query:     `ALTER ROLE %s SET %s TO %s;`,
				QueryArgs: []any{identifier, pgx.Identifier{k}, v},
			})
		}
	}

	return
}

func (r *Role) Create() (out []postgres.SyncQuery) {
	identifier := pgx.Identifier{r.Name}

	if r.BeforeCreate != "" {
		out = append(out, postgres.SyncQuery{
			Description: "Run before create hook.",
			LogArgs:     []any{"role", r.Name, "sql", r.BeforeCreate},
			Query:       r.BeforeCreate,
		})
	}

	if len(r.Parents) > 0 {
		parents := []any{}
		for _, parent := range r.Parents {
			parents = append(parents, pgx.Identifier{parent.Name})
		}
		out = append(out, postgres.SyncQuery{
			Description: "Create role.",
			LogArgs:     []any{"role", r.Name, "parents", r.Parents},
			Query: `
			CREATE ROLE %s
			WITH ` + r.Options.String() + `
			IN ROLE %s;`,
			QueryArgs: []any{identifier, parents},
		})
	} else {
		out = append(out, postgres.SyncQuery{
			Description: "Create role.",
			LogArgs:     []any{"role", r.Name},
			Query:       `CREATE ROLE %s WITH ` + r.Options.String() + `;`,
			QueryArgs:   []any{identifier},
		})
	}
	out = append(out, postgres.SyncQuery{
		Description: "Set role comment.",
		LogArgs:     []any{"role", r.Name},
		Query:       `COMMENT ON ROLE %s IS %s;`,
		QueryArgs:   []any{identifier, r.Comment},
	})

	if r.Config != nil {
		for k, v := range *r.Config {
			out = append(out, postgres.SyncQuery{
				Description: "Set role config.",
				LogArgs:     []any{"role", r.Name, "config", k, "value", v},
				Query:       `ALTER ROLE %s SET %s TO %s`,
				QueryArgs:   []any{identifier, pgx.Identifier{k}, v},
			})
		}
	}

	if r.AfterCreate != "" {
		out = append(out, postgres.SyncQuery{
			Description: "Run after create hook.",
			LogArgs:     []any{"role", r.Name, "sql", r.AfterCreate},
			Query:       r.AfterCreate,
		})
	}

	return
}

func (r *Role) Drop(fallbackOwner string) (out []postgres.SyncQuery) {
	identifier := pgx.Identifier{r.Name}
	if r.Options.CanLogin {
		out = append(out, postgres.SyncQuery{
			Description: "Terminate running sessions.",
			LogArgs:     []any{"role", r.Name},
			Database:    "<first>",
			Query: `
			SELECT pg_terminate_backend(pid)
			FROM pg_catalog.pg_stat_activity
			WHERE usename = %s;`,
			QueryArgs: []any{r.Name},
		})
	}

	for dbname, database := range postgres.Databases {
		if database.Owner == r.Name {
			out = append(out, postgres.SyncQuery{
				Description: "Reassign database.",
				LogArgs: []any{
					"database", database.Name,
					"old", r.Name,
					"new", fallbackOwner,
				},
				Query: `ALTER DATABASE %s OWNER TO %s;`,
				QueryArgs: []any{
					pgx.Identifier{database.Name},
					pgx.Identifier{fallbackOwner},
				},
			})
			// Update model to generate propery queries next.
			database.Owner = fallbackOwner
			postgres.Databases[dbname] = database
		}
		out = append(out, postgres.SyncQuery{
			Description: "Reassign objects and purge ACL.",
			LogArgs: []any{
				"role", r.Name, "owner", database.Owner,
			},
			Database: database.Name,
			Query: `
			REASSIGN OWNED BY %s TO %s;
			DROP OWNED BY %s;`,
			QueryArgs: []any{
				identifier, pgx.Identifier{database.Owner}, identifier,
			},
		})
	}
	out = append(out, postgres.SyncQuery{
		Description: "Drop role.",
		LogArgs:     []any{"role", r.Name},
		Query:       `DROP ROLE %s;`,
		QueryArgs:   []any{identifier},
	})
	return
}

func (r *Role) Merge(o Role) {
	for _, membership := range o.Parents {
		if r.MemberOf(membership.Name) {
			continue
		}
		r.Parents = append(r.Parents, membership)
	}
}
