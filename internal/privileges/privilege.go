package privileges

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/dalibo/ldap2pg/v6/internal/normalize"
)

// Privilege references a privilege type and an ACL
//
// Example: {Type: "CONNECT", To: "DATABASE"}
type Privilege struct {
	Type   string // Privilege type (USAGE, etc.)
	On     string // ACL (DATABASE, GLOBAL DEFAULT, etc)
	Object string // TABLES, SCHEMAS, etc.
}

func (p Privilege) ACL() string {
	return p.On
}

func NormalizePrivilege(rawPrivilege any) (any, error) {
	m, ok := rawPrivilege.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bad type")
	}

	// DEPRECATED: v6.2 compat
	def, _ := m["default"].(string)
	if def != "" {
		// 6.2 has only scalar type.
		m["object"] = m["type"]
		m["on"] = fmt.Sprintf("%s DEFAULT", strings.ToUpper(def))
		delete(m, "default")
		slog.Warn("Deprecated default scope.")
		slog.Warn("Use 'object' instead of 'default' in privilege definition.", "on", m["on"], "object", m["object"])
	}

	err := normalize.Alias(m, "types", "type")
	if err != nil {
		return m, err
	}
	m["types"] = normalize.List(m["types"])

	err = normalize.SpuriousKeys(m, "types", "on", "object")

	return m, err
}

func DuplicatePrivilege(yaml map[string]any) (privileges []any) {
	for _, singleType := range yaml["types"].([]any) {
		privilege := make(map[string]any)
		privilege["type"] = singleType
		for key, value := range yaml {
			if key == "types" {
				continue
			}
			privilege[key] = value
		}
		privileges = append(privileges, privilege)
	}
	return
}
