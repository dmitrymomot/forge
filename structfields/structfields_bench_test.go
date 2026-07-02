package structfields_test

import (
	"testing"

	"github.com/dmitrymomot/forge/structfields"
)

type benchStruct struct {
	Name    string `json:"name,omitempty"`
	Email   string `json:"email"`
	Age     int    `json:"age,omitempty"`
	Active  bool   `json:"active"`
	skipped string //nolint:unused // exercises the unexported-field skip path
}

func BenchmarkWalk(b *testing.B) {
	v := &benchStruct{}
	b.ReportAllocs()
	for b.Loop() {
		_ = structfields.Walk(v, "json", func(structfields.Field) error {
			return nil
		})
	}
}

func BenchmarkWalkHasOption(b *testing.B) {
	v := benchStruct{}
	b.ReportAllocs()
	for b.Loop() {
		_ = structfields.Walk(v, "json", func(f structfields.Field) error {
			_ = f.Tag.HasOption("omitempty")
			return nil
		})
	}
}

func BenchmarkFieldSet(b *testing.B) {
	v := &benchStruct{}
	b.ReportAllocs()
	for b.Loop() {
		_ = structfields.Walk(v, "json", func(f structfields.Field) error {
			switch f.Name {
			case "Name", "Email":
				return f.Set("value")
			case "Age":
				return f.Set(42)
			case "Active":
				return f.Set(true)
			}
			return nil
		})
	}
}
