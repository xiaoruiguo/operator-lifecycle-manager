package solver

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestInstallable struct {
	identifier  Identifier
	constraints []Constraint
}

func (i TestInstallable) Identifier() Identifier {
	return i.identifier
}

func (i TestInstallable) Constraints() []Constraint {
	return i.constraints
}

func (i TestInstallable) GoString() string {
	return fmt.Sprintf("%q", i.Identifier())
}

func installable(id Identifier, constraints ...Constraint) Installable {
	return TestInstallable{
		identifier:  id,
		constraints: constraints,
	}
}

func TestNotSatisfiableError(t *testing.T) {
	type tc struct {
		Name   string
		Error  NotSatisfiable
		String string
	}

	for _, tt := range []tc{
		{
			Name:   "nil",
			String: "constraints not satisfiable",
		},
		{
			Name:   "empty",
			String: "constraints not satisfiable",
			Error:  NotSatisfiable{},
		},
		{
			Name: "single failure",
			Error: NotSatisfiable{
				AppliedConstraint{
					Installable: installable("a", Mandatory()),
					Constraint:  Mandatory(),
				},
			},
			String: fmt.Sprintf("constraints not satisfiable: %s",
				Mandatory().String("a")),
		},
		{
			Name: "multiple failures",
			Error: NotSatisfiable{
				AppliedConstraint{
					Installable: installable("a", Mandatory()),
					Constraint:  Mandatory(),
				},
				AppliedConstraint{
					Installable: installable("b", Prohibited()),
					Constraint:  Prohibited(),
				},
			},
			String: fmt.Sprintf("constraints not satisfiable: %s, %s",
				Mandatory().String("a"), Prohibited().String("b")),
		},
	} {
		t.Run(tt.Name, func(t *testing.T) {
			assert.Equal(t, tt.String, tt.Error.Error())
		})
	}
}

func TestSolve(t *testing.T) {
	type tc struct {
		Name         string
		Installables []Installable
		Installed    []Installable
		Error        error
	}

	for _, tt := range []tc{
		{
			Name: "no installables",
		},
		{
			Name:         "unnecessary installable is not installed",
			Installables: []Installable{installable("a")},
		},
		{
			Name:         "single mandatory installable is installed",
			Installables: []Installable{installable("a", Mandatory())},
			Installed:    []Installable{installable("a", Mandatory())},
		},
		{
			Name:         "both mandatory and prohibited produce error",
			Installables: []Installable{installable("a", Mandatory(), Prohibited())},
			Error: NotSatisfiable{
				{
					Installable: installable("a", Mandatory(), Prohibited()),
					Constraint:  Mandatory(),
				},
				{
					Installable: installable("a", Mandatory(), Prohibited()),
					Constraint:  Prohibited(),
				},
			},
		},
		{
			Name: "dependency is installed",
			Installables: []Installable{
				installable("a"),
				installable("b", Mandatory(), Dependency("a")),
			},
			Installed: []Installable{
				installable("a"),
				installable("b", Mandatory(), Dependency("a")),
			},
		},
		{
			Name: "transitive dependency is installed",
			Installables: []Installable{
				installable("a"),
				installable("b", Dependency("a")),
				installable("c", Mandatory(), Dependency("b")),
			},
			Installed: []Installable{
				installable("a"),
				installable("b", Dependency("a")),
				installable("c", Mandatory(), Dependency("b")),
			},
		},
		{
			Name: "both dependencies are installed",
			Installables: []Installable{
				installable("a"),
				installable("b"),
				installable("c", Mandatory(), Dependency("a"), Dependency("b")),
			},
			Installed: []Installable{
				installable("a"),
				installable("b"),
				installable("c", Mandatory(), Dependency("a"), Dependency("b")),
			},
		},
		{
			Name: "solution with first dependency is selected",
			Installables: []Installable{
				installable("a"),
				installable("b", Conflict("a")),
				installable("c", Mandatory(), Dependency("a", "b")),
			},
			Installed: []Installable{
				installable("a"),
				installable("c", Mandatory(), Dependency("a", "b")),
			},
		},
		{
			Name: "solution with only first dependency is selected",
			Installables: []Installable{
				installable("a"),
				installable("b"),
				installable("c", Mandatory(), Dependency("a", "b")),
			},
			Installed: []Installable{
				installable("a"),
				installable("c", Mandatory(), Dependency("a", "b")),
			},
		},
		{
			Name: "solution with first dependency is selected (reverse)",
			Installables: []Installable{
				installable("a"),
				installable("b", Conflict("a")),
				installable("c", Mandatory(), Dependency("b", "a")),
			},
			Installed: []Installable{
				installable("b", Conflict("a")),
				installable("c", Mandatory(), Dependency("b", "a")),
			},
		},
		{
			Name: "two mandatory but conflicting packages",
			Installables: []Installable{
				installable("a", Mandatory()),
				installable("b", Mandatory(), Conflict("a")),
			},
			Error: NotSatisfiable{
				{
					Installable: installable("a", Mandatory()),
					Constraint:  Mandatory(),
				},
				{
					Installable: installable("b", Mandatory(), Conflict("a")),
					Constraint:  Mandatory(),
				},
				{
					Installable: installable("b", Mandatory(), Conflict("a")),
					Constraint:  Conflict("a"),
				},
			},
		},
		{
			Name: "irrelevant dependencies don't influence search order",
			Installables: []Installable{
				installable("a", Dependency("x", "y")),
				installable("b", Mandatory(), Dependency("y", "x")),
				installable("x"),
				installable("y"),
			},
			Installed: []Installable{
				installable("b", Mandatory(), Dependency("y", "x")),
				installable("y"),
			},
		},
		{
			Name: "cardinality constraint prevents resolution",
			Installables: []Installable{
				installable("a", Mandatory(), Dependency("x", "y"), AtMost(1, "x", "y")),
				installable("x", Mandatory()),
				installable("y", Mandatory()),
			},
			Error: NotSatisfiable{
				{
					Installable: installable("a", Mandatory(), Dependency("x", "y"), AtMost(1, "x", "y")),
					Constraint:  AtMost(1, "x", "y"),
				},
				{
					Installable: installable("x", Mandatory()),
					Constraint:  Mandatory(),
				},
				{
					Installable: installable("y", Mandatory()),
					Constraint:  Mandatory(),
				},
			},
		},
		{
			Name: "cardinality constraint forces alternative",
			Installables: []Installable{
				installable("a", Mandatory(), Dependency("x", "y"), AtMost(1, "x", "y")),
				installable("b", Mandatory(), Dependency("y")),
				installable("x"),
				installable("y"),
			},
			Installed: []Installable{
				installable("a", Mandatory(), Dependency("x", "y"), AtMost(1, "x", "y")),
				installable("b", Mandatory(), Dependency("y")),
				installable("y"),
			},
		},
		{
			Name: "two dependencies satisfied by one installable",
			Installables: []Installable{
				installable("a", Mandatory(), Dependency("y")),
				installable("b", Mandatory(), Dependency("x", "y")),
				installable("x"),
				installable("y"),
			},
			Installed: []Installable{
				installable("a", Mandatory(), Dependency("y")),
				installable("b", Mandatory(), Dependency("x", "y")),
				installable("y"),
			},
		},
		{
			Name: "result size larger than minimum due to preference",
			Installables: []Installable{
				installable("a", Mandatory(), Dependency("x", "y")),
				installable("b", Mandatory(), Dependency("y")),
				installable("x"),
				installable("y"),
			},
			Installed: []Installable{
				installable("a", Mandatory(), Dependency("x", "y")),
				installable("b", Mandatory(), Dependency("y")),
				installable("x"),
				installable("y"),
			},
		},
		{
			Name: "only the least preferable choice is acceptable",
			Installables: []Installable{
				installable("a", Mandatory(), Dependency("a1", "a2")),
				installable("a1", Conflict("c1"), Conflict("c2")),
				installable("a2", Conflict("c1")),
				installable("b", Mandatory(), Dependency("b1", "b2")),
				installable("b1", Conflict("c1"), Conflict("c2")),
				installable("b2", Conflict("c1")),
				installable("c", Mandatory(), Dependency("c1", "c2")),
				installable("c1"),
				installable("c2"),
			},
			Installed: []Installable{
				installable("a", Mandatory(), Dependency("a1", "a2")),
				installable("a2", Conflict("c1")),
				installable("b", Mandatory(), Dependency("b1", "b2")),
				installable("b2", Conflict("c1")),
				installable("c", Mandatory(), Dependency("c1", "c2")),
				installable("c2"),
			},
		},
	} {
		t.Run(tt.Name, func(t *testing.T) {
			assert := assert.New(t)

			var traces bytes.Buffer
			s, err := New(WithInput(tt.Installables), WithTracer(LoggingTracer{Writer: &traces}))
			if err != nil {
				t.Fatalf("failed to initialize solver: %s", err)
			}

			installed, err := s.Solve(context.TODO())

			if installed != nil {
				sort.SliceStable(installed, func(i, j int) bool {
					return installed[i].Identifier() < installed[j].Identifier()
				})
			}

			// Failed constraints are sorted in lexically
			// increasing order of the identifier of the
			// constraint's installable, with ties broken
			// in favor of the constraint that appears
			// earliest in the installable's list of
			// constraints.
			if ns, ok := err.(NotSatisfiable); ok {
				sort.SliceStable(ns, func(i, j int) bool {
					if ns[i].Installable.Identifier() != ns[j].Installable.Identifier() {
						return ns[i].Installable.Identifier() < ns[j].Installable.Identifier()
					}
					var x, y int
					for ii, c := range ns[i].Installable.Constraints() {
						if reflect.DeepEqual(c, ns[i].Constraint) {
							x = ii
							break
						}
					}
					for ij, c := range ns[j].Installable.Constraints() {
						if reflect.DeepEqual(c, ns[j].Constraint) {
							y = ij
							break
						}
					}
					return x < y
				})
			}

			assert.Equal(tt.Installed, installed)
			assert.Equal(tt.Error, err)

			if t.Failed() {
				t.Logf("\n%s", traces.String())
			}
		})
	}
}

// TODO: the search tests should verify that by the time we're done searching and returned
// to the CardSort, we're no longer in a deeper test context than we should be.
//func TestSearch(t *testing.T) {
//
//}
