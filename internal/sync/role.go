package sync

import (
	"github.com/dalibo/ldap2pg/internal/ldap"
	"github.com/dalibo/ldap2pg/internal/lists"
	"github.com/dalibo/ldap2pg/internal/pyfmt"
	"github.com/dalibo/ldap2pg/internal/role"
	mapset "github.com/deckarep/golang-set/v2"
)

type RoleRule struct {
	Name    pyfmt.Format
	Options role.Options
	Comment pyfmt.Format
	Parents []pyfmt.Format
}

func (r RoleRule) IsStatic() bool {
	return r.Name.IsStatic() &&
		r.Comment.IsStatic() &&
		lists.And(r.Parents, func(f pyfmt.Format) bool { return f.IsStatic() })
}

func (r RoleRule) Generate(results *ldap.Result) <-chan role.Role {
	ch := make(chan role.Role)
	go func() {
		defer close(ch)
		parents := mapset.NewSet[string]()
		for _, f := range r.Parents {
			if nil == results.Entry || 0 == len(f.Fields) {
				// Static case.
				parents.Add(f.String())
			} else {
				// Dynamic case.
				for values := range results.GenerateValues(f) {
					parents.Add(f.Format(values))
				}
			}
		}

		if nil == results.Entry {
			// Case static rule.
			role := role.Role{
				Name:    r.Name.String(),
				Comment: r.Comment.String(),
				Options: r.Options,
				Parents: parents,
			}
			ch <- role
		} else {
			// Case dynamic rule.
			for values := range results.GenerateValues(r.Name, r.Comment) {
				role := role.Role{}
				role.Name = r.Name.Format(values)
				role.Comment = r.Comment.Format(values)
				role.Options = r.Options
				role.Parents = parents.Clone()
				ch <- role
			}
		}
	}()
	return ch
}