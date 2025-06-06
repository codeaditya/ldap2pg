package privileges

import (
	"fmt"
	"strings"

	"github.com/dalibo/ldap2pg/v6/internal/ldap"
	"github.com/dalibo/ldap2pg/v6/internal/lists"
	"github.com/dalibo/ldap2pg/v6/internal/normalize"
	"github.com/dalibo/ldap2pg/v6/internal/pyfmt"
	"golang.org/x/exp/maps"
)

// NormalizeGrantRule from loose YAML
//
// Sets default values. Checks some conflicts.
// Hormonize types for DuplicateGrantRules.
func NormalizeGrantRule(yaml any) (rule map[string]any, err error) {
	rule = map[string]any{
		"owners":    "__auto__",
		"schemas":   "__all__",
		"databases": "__all__",
	}

	yamlMap, ok := yaml.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bad type")
	}

	err = normalize.Alias(yamlMap, "owners", "owner")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "privileges", "privilege")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "databases", "database")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "schemas", "schema")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "roles", "to")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "roles", "grantee")
	if err != nil {
		return
	}
	err = normalize.Alias(yamlMap, "roles", "role")
	if err != nil {
		return
	}

	maps.Copy(rule, yamlMap)

	keys := []string{"owners", "privileges", "databases", "schemas", "roles"}
	for _, k := range keys {
		rule[k], err = normalize.StringList(rule[k])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
	}
	err = normalize.SpuriousKeys(rule, keys...)
	return
}

// DuplicateGrantRules split plurals for mapstructure
func DuplicateGrantRules(yaml map[string]any) (rules []any) {
	keys := []string{"owners", "databases", "schemas", "roles", "privileges"}
	keys = lists.Filter(keys, func(s string) bool {
		return len(yaml[s].([]string)) > 0
	})
	fields := [][]string{}
	for _, k := range keys {
		fields = append(fields, yaml[k].([]string))
	}
	for combination := range lists.Product(fields...) {
		rule := map[string]any{}
		for i, k := range keys {
			rule[strings.TrimSuffix(k, "s")] = combination[i]
		}
		rules = append(rules, rule)
	}
	return
}

// GrantRule is a template to generate wanted GRANTS from data
//
// data comes from LDAP search result or static configuration.
type GrantRule struct {
	Owner     pyfmt.Format
	Privilege pyfmt.Format
	Database  pyfmt.Format
	Schema    pyfmt.Format
	To        pyfmt.Format `mapstructure:"role"`
}

func (r GrantRule) IsStatic() bool {
	return lists.And(r.Formats(), func(f pyfmt.Format) bool { return f.IsStatic() })
}

func (r GrantRule) Formats() []pyfmt.Format {
	return []pyfmt.Format{r.Owner, r.Privilege, r.Database, r.Schema, r.To}
}

func (r GrantRule) Generate(results *ldap.Result) <-chan Grant {
	ch := make(chan Grant)
	go func() {
		defer close(ch)

		var vchan <-chan map[string]string
		if nil == results.Entry {
			// Create a single-value chan.
			vchanw := make(chan map[string]string, 1)
			vchanw <- nil
			close(vchanw)
			vchan = vchanw
		} else {
			vchan = results.GenerateValues(r.Owner, r.Privilege, r.Database, r.Schema, r.To)
		}

		for values := range vchan {
			profile := r.Privilege.Format(values)
			for _, priv := range profiles[profile] {
				acl := acls[priv.ACL()]
				grant := Grant{
					ACL:     priv.On,
					Grantee: r.To.Format(values),
					Type:    priv.Type,
				}

				if acl.Uses("owner") {
					grant.Owner = r.Owner.Format(values)
				}

				if acl.Uses("schema") {
					grant.Schema = r.Schema.Format(values)
				}

				if acl.Uses("object") {
					grant.Object = priv.Object
				}

				if acl.Scope != "instance" || acl.Uses("database") {
					grant.Database = r.Database.Format(values)
				}

				ch <- grant
			}
		}
	}()
	return ch
}
