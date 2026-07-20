package abac_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/abac"
	"github.com/dmitrymomot/forge/auth/access"
)

func evalPred(t *testing.T, p abac.Predicate, s access.Subject, r access.Resource) (bool, error) {
	t.Helper()
	return p(context.Background(), s, r)
}

func TestAttr(t *testing.T) {
	t.Parallel()

	attrs := map[string]any{"owner_id": "u1", "count": 3, "archived": true}

	if v, ok := abac.Attr[string](attrs, "owner_id"); !ok || v != "u1" {
		t.Fatalf("string attr = %q %v", v, ok)
	}
	if v, ok := abac.Attr[int](attrs, "count"); !ok || v != 3 {
		t.Fatalf("int attr = %d %v", v, ok)
	}
	if _, ok := abac.Attr[string](attrs, "count"); ok {
		t.Fatal("wrong type must not match")
	}
	if _, ok := abac.Attr[string](attrs, "missing"); ok {
		t.Fatal("missing key must not match")
	}
	if _, ok := abac.Attr[string](nil, "owner_id"); ok {
		t.Fatal("nil map must not match")
	}
}

func TestOwner(t *testing.T) {
	t.Parallel()

	pred := abac.Owner("owner_id")
	owned := access.Resource{Attrs: map[string]any{"owner_id": "u1"}}

	tests := []struct {
		name     string
		subject  access.Subject
		resource access.Resource
		want     bool
	}{
		{"owner matches", access.Subject{ID: "u1"}, owned, true},
		{"other subject", access.Subject{ID: "u2"}, owned, false},
		{"missing attr", access.Subject{ID: "u1"}, access.Resource{}, false},
		{"non-string attr", access.Subject{ID: "u1"}, access.Resource{Attrs: map[string]any{"owner_id": 7}}, false},
		{"empty owner and empty subject", access.Subject{}, access.Resource{Attrs: map[string]any{"owner_id": ""}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := evalPred(t, pred, tt.subject, tt.resource)
			if err != nil || got != tt.want {
				t.Fatalf("Owner = %v %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestAnd(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	errPred := abac.Predicate(func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		return true, sentinel
	})
	panicPred := abac.Predicate(func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		panic("must not be evaluated")
	})

	if ok, err := evalPred(t, abac.And(truePred, truePred), access.Subject{}, access.Resource{}); err != nil || !ok {
		t.Fatalf("all true = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.And(falsePred, panicPred), access.Subject{}, access.Resource{}); err != nil || ok {
		t.Fatalf("short-circuit on false = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.And(errPred, panicPred), access.Subject{}, access.Resource{}); !errors.Is(err, sentinel) || ok {
		t.Fatalf("error propagation = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.And(), access.Subject{}, access.Resource{}); err != nil || ok {
		t.Fatalf("empty And must be false (fail closed) = %v %v", ok, err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("nil predicate must panic at wiring time")
		}
	}()
	abac.And(truePred, nil)
}

func TestOr(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	errPred := abac.Predicate(func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		return false, sentinel
	})
	panicPred := abac.Predicate(func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		panic("must not be evaluated")
	})

	if ok, err := evalPred(t, abac.Or(truePred, panicPred), access.Subject{}, access.Resource{}); err != nil || !ok {
		t.Fatalf("short-circuit on true = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.Or(falsePred, falsePred), access.Subject{}, access.Resource{}); err != nil || ok {
		t.Fatalf("all false = %v %v", ok, err)
	}
	// Fail closed: an error wins over a later true.
	if ok, err := evalPred(t, abac.Or(errPred, truePred), access.Subject{}, access.Resource{}); !errors.Is(err, sentinel) || ok {
		t.Fatalf("error propagation = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.Or(), access.Subject{}, access.Resource{}); err != nil || ok {
		t.Fatalf("empty Or must be false = %v %v", ok, err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("nil predicate must panic at wiring time")
		}
	}()
	abac.Or(nil)
}

func TestNot(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	errPred := abac.Predicate(func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		return true, sentinel
	})

	if ok, err := evalPred(t, abac.Not(falsePred), access.Subject{}, access.Resource{}); err != nil || !ok {
		t.Fatalf("Not(false) = %v %v", ok, err)
	}
	if ok, err := evalPred(t, abac.Not(truePred), access.Subject{}, access.Resource{}); err != nil || ok {
		t.Fatalf("Not(true) = %v %v", ok, err)
	}
	// Fail closed: an error is not inverted into a grant.
	if ok, err := evalPred(t, abac.Not(errPred), access.Subject{}, access.Resource{}); !errors.Is(err, sentinel) || ok {
		t.Fatalf("Not(error) = %v %v", ok, err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("nil predicate must panic at wiring time")
		}
	}()
	abac.Not(nil)
}
