package dynaml

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/mandelsoft/spiff/yaml"
)

var _ = Describe("dynamic references", func() {
	Context("when a dynamic string reference is found", func() {
		It("evaluates to the map entry", func() {
			ref := ReferenceExpr{Path: []string{"foo"}}
			idx := StringExpr{"bar"}
			expr := DynamicExpr{ref, idx}

			binding := FakeBinding{
				FoundReferences: map[string]yaml.Node{
					"foo": NewNode(map[string]yaml.Node{
						"bar": NewNode(42, nil),
					}, nil),
				},
			}

			Expect(expr).To(EvaluateAs(42, binding))
		})
	})

	Context("when a dynamic array reference is found", func() {
		It("evaluates to the indexed array entry", func() {
			ref := ReferenceExpr{Path: []string{"foo"}}
			idx := IntegerExpr{1}
			expr := DynamicExpr{ref, idx}
			binding := FakeBinding{
				FoundReferences: map[string]yaml.Node{
					"foo": NewNode([]yaml.Node{NewNode(1, nil), NewNode(42, nil)}, nil),
				},
			}

			Expect(expr).To(EvaluateAs(42, binding))
		})

		It("evaluates to the indexed array entry", func() {
			ref := ReferenceExpr{Path: []string{"foo"}}
			idx := ListExpr{[]Expression{IntegerExpr{1}}}
			expr := DynamicExpr{ref, idx}
			binding := FakeBinding{
				FoundReferences: map[string]yaml.Node{
					"foo": NewNode([]yaml.Node{NewNode(1, nil), NewNode(42, nil)}, nil),
				},
			}

			Expect(expr).To(EvaluateAs(42, binding))
		})

		It("evaluates to the multi-indexed array entry", func() {
			ref := ReferenceExpr{Path: []string{"foo"}}
			idx := ListExpr{[]Expression{IntegerExpr{0}, IntegerExpr{1}}}
			expr := DynamicExpr{ref, idx}
			binding := FakeBinding{
				FoundReferences: map[string]yaml.Node{
					"foo": NewNode([]yaml.Node{NewNode([]yaml.Node{NewNode(1, nil), NewNode(42, nil)}, nil)}, nil),
				},
			}

			Expect(expr).To(EvaluateAs(42, binding))
		})
	})
})
