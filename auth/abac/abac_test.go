package abac_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/auth/abac"
	"github.com/dmitrymomot/forge/auth/access"
)

func truePred(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
	return true, nil
}

func falsePred(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
	return false, nil
}

func mustPolicy(t *testing.T, rules ...abac.Rule) *abac.Policy {
	t.Helper()
	p, err := abac.New(abac.WithRules(rules...))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

func decide(t *testing.T, p *abac.Policy, s access.Subject, a access.Action, r access.Resource) access.Decision {
	t.Helper()
	dec, err := p.Decide(context.Background(), s, a, r)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	return dec
}

func TestNewValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rule abac.Rule
		want error
	}{
		{"zero rule", abac.Rule{}, abac.ErrUnnamedRule},
		{"unnamed", abac.Allow("", "documents:read", "document", truePred), abac.ErrUnnamedRule},
		{"nil predicate", abac.Allow("r", "documents:read", "document", nil), abac.ErrNilPredicate},
		{"empty action", abac.Allow("r", "", "document", truePred), abac.ErrEmptyAction},
		{"empty resource", abac.Allow("r", "documents:read", "", truePred), abac.ErrEmptyResource},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := abac.New(abac.WithRules(tt.rule)); !errors.Is(err, tt.want) {
				t.Fatalf("New error = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("duplicate name", func(t *testing.T) {
		t.Parallel()
		// Accumulation across WithRules calls still detects the duplicate.
		_, err := abac.New(
			abac.WithRules(abac.Allow("dup", "documents:read", "document", truePred)),
			abac.WithRules(abac.Deny("dup", "documents:write", "*", truePred)),
		)
		if !errors.Is(err, abac.ErrDuplicateRule) {
			t.Fatalf("New error = %v, want ErrDuplicateRule", err)
		}
	})

	t.Run("empty policy is valid and abstains", func(t *testing.T) {
		t.Parallel()
		p := mustPolicy(t)
		dec := decide(t, p, access.Subject{}, "documents:read", access.Resource{})
		if dec.Effect != access.Abstain {
			t.Fatalf("Effect = %v, want abstain", dec.Effect)
		}
	})
}

func TestDecideActionMatching(t *testing.T) {
	t.Parallel()

	p := mustPolicy(t,
		abac.Allow("exact", "documents:read", "*", truePred),
		abac.Allow("noun", "reports:*", "*", truePred),
		abac.Allow("super", "*", "special", truePred),
	)

	tests := []struct {
		name     string
		action   access.Action
		resource access.Resource
		effect   access.Effect
		reason   string
	}{
		{"exact hit", "documents:read", access.Resource{Type: "document"}, access.Allow, `allowed by rule "exact"`},
		{"exact miss", "documents:write", access.Resource{Type: "document"}, access.Abstain, "no rule matched"},
		{"noun wildcard hit", "reports:export", access.Resource{Type: "report"}, access.Allow, `allowed by rule "noun"`},
		{"noun wildcard needs separator", "reports", access.Resource{Type: "report"}, access.Abstain, "no rule matched"},
		{"noun wildcard other noun", "players:read", access.Resource{Type: "report"}, access.Abstain, "no rule matched"},
		{"super matches anything", "anything:at:all", access.Resource{Type: "special"}, access.Allow, `allowed by rule "super"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec := decide(t, p, access.Subject{}, tt.action, tt.resource)
			if dec.Effect != tt.effect {
				t.Fatalf("Effect = %v, want %v", dec.Effect, tt.effect)
			}
			if dec.Reason != tt.reason {
				t.Fatalf("Reason = %q, want %q", dec.Reason, tt.reason)
			}
			if dec.Decider != "abac" {
				t.Fatalf("Decider = %q, want abac", dec.Decider)
			}
		})
	}
}

func TestDecideResourceMatching(t *testing.T) {
	t.Parallel()

	p := mustPolicy(t,
		abac.Allow("typed", "documents:read", "document", truePred),
		abac.Allow("any", "reports:read", "*", truePred),
	)

	tests := []struct {
		name     string
		action   access.Action
		resource access.Resource
		effect   access.Effect
	}{
		{"exact type hit", "documents:read", access.Resource{Type: "document"}, access.Allow},
		{"type mismatch abstains", "documents:read", access.Resource{Type: "folder"}, access.Abstain},
		{"empty type does not match exact", "documents:read", access.Resource{}, access.Abstain},
		{"star matches any type", "reports:read", access.Resource{Type: "report"}, access.Allow},
		{"star matches empty type", "reports:read", access.Resource{}, access.Allow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dec := decide(t, p, access.Subject{}, tt.action, tt.resource)
			if dec.Effect != tt.effect {
				t.Fatalf("Effect = %v, want %v", dec.Effect, tt.effect)
			}
		})
	}
}

func TestDecideDenyBeforeAllow(t *testing.T) {
	t.Parallel()

	// Deny registered AFTER the allow still vetoes: deny rules evaluate first.
	p := mustPolicy(t,
		abac.Allow("grant", "documents:read", "document", truePred),
		abac.Deny("veto", "documents:*", "document", truePred),
	)
	dec := decide(t, p, access.Subject{}, "documents:read", access.Resource{Type: "document"})
	if dec.Effect != access.Deny {
		t.Fatalf("Effect = %v, want deny", dec.Effect)
	}
	if dec.Reason != `denied by rule "veto"` {
		t.Fatalf("Reason = %q", dec.Reason)
	}

	// An unsatisfied deny falls through to the allow.
	p = mustPolicy(t,
		abac.Deny("veto", "documents:*", "document", falsePred),
		abac.Allow("grant", "documents:read", "document", truePred),
	)
	dec = decide(t, p, access.Subject{}, "documents:read", access.Resource{Type: "document"})
	if dec.Effect != access.Allow {
		t.Fatalf("Effect = %v, want allow", dec.Effect)
	}
}

func TestDecideFirstSatisfiedAllowWins(t *testing.T) {
	t.Parallel()

	p := mustPolicy(t,
		abac.Allow("first", "documents:read", "*", falsePred),
		abac.Allow("second", "documents:read", "*", truePred),
		abac.Allow("third", "documents:read", "*", truePred),
	)
	dec := decide(t, p, access.Subject{}, "documents:read", access.Resource{})
	if dec.Reason != `allowed by rule "second"` {
		t.Fatalf("Reason = %q, want second rule", dec.Reason)
	}
}

func TestDecideAbstainReasons(t *testing.T) {
	t.Parallel()

	p := mustPolicy(t, abac.Allow("grant", "documents:read", "document", falsePred))

	dec := decide(t, p, access.Subject{}, "other:action", access.Resource{Type: "document"})
	if dec.Effect != access.Abstain || dec.Reason != "no rule matched" {
		t.Fatalf("got %v %q, want abstain %q", dec.Effect, dec.Reason, "no rule matched")
	}

	dec = decide(t, p, access.Subject{}, "documents:read", access.Resource{Type: "document"})
	if dec.Effect != access.Abstain || dec.Reason != "no predicate satisfied" {
		t.Fatalf("got %v %q, want abstain %q", dec.Effect, dec.Reason, "no predicate satisfied")
	}
}

func TestDecidePredicateError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("relationship store down")
	errPred := func(_ context.Context, _ access.Subject, _ access.Resource) (bool, error) {
		return true, sentinel
	}

	for _, tt := range []struct {
		name string
		rule abac.Rule
	}{
		{"allow rule error", abac.Allow("broken", "documents:read", "*", errPred)},
		{"deny rule error", abac.Deny("broken", "documents:read", "*", errPred)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := mustPolicy(t, tt.rule)
			dec, err := p.Decide(context.Background(), access.Subject{}, "documents:read", access.Resource{})
			if !errors.Is(err, sentinel) {
				t.Fatalf("err = %v, want wrapped sentinel", err)
			}
			if !strings.Contains(err.Error(), `"broken"`) {
				t.Fatalf("err %q does not name the rule", err)
			}
			if dec.Effect != access.Abstain {
				t.Fatalf("Effect = %v, want abstain (seam fails it closed)", dec.Effect)
			}
		})
	}

	t.Run("error stops evaluation before later rules", func(t *testing.T) {
		t.Parallel()
		p := mustPolicy(t,
			abac.Allow("broken", "documents:read", "*", errPred),
			abac.Allow("grant", "documents:read", "*", truePred),
		)
		if _, err := p.Decide(context.Background(), access.Subject{}, "documents:read", access.Resource{}); !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want sentinel", err)
		}
	})
}

func TestDecideInSeamChain(t *testing.T) {
	t.Parallel()

	policy := mustPolicy(t,
		abac.Allow("own-document", "documents:*", "document", abac.Owner("owner_id")),
	)
	d := access.FirstDecisive(access.TenantMatch(), policy, access.ScopeDecider())

	owned := access.Resource{Type: "document", ID: "d1", Attrs: map[string]any{"owner_id": "u1"}}

	// Ownership grants without any scope.
	dec, err := access.Authorize(context.Background(), d, access.Subject{ID: "u1"}, "documents:write", owned)
	if err != nil || dec.Effect != access.Allow {
		t.Fatalf("got %v %v, want allow", dec.Effect, err)
	}

	// Non-owner without a scope: abac abstains, chain closes to deny.
	dec, err = access.Authorize(context.Background(), d, access.Subject{ID: "u2"}, "documents:write", owned)
	if err != nil || dec.Effect != access.Deny {
		t.Fatalf("got %v %v, want deny", dec.Effect, err)
	}

	// Cross-tenant veto beats ownership: TenantMatch sits above abac.
	crossTenant := access.Resource{Type: "document", ID: "d1", Tenant: "t2", Attrs: map[string]any{"owner_id": "u1"}}
	dec, err = access.Authorize(context.Background(), d, access.Subject{ID: "u1", Tenant: "t1"}, "documents:write", crossTenant)
	if err != nil || dec.Effect != access.Deny {
		t.Fatalf("got %v %v, want deny", dec.Effect, err)
	}
}

func TestDecideConcurrent(t *testing.T) {
	t.Parallel()

	p := mustPolicy(t,
		abac.Deny("veto", "documents:delete", "document", truePred),
		abac.Allow("own-document", "documents:*", "document", abac.Owner("owner_id")),
	)
	res := access.Resource{Type: "document", Attrs: map[string]any{"owner_id": "u1"}}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 500 {
				dec, err := p.Decide(context.Background(), access.Subject{ID: "u1"}, "documents:read", res)
				if err != nil || dec.Effect != access.Allow {
					t.Errorf("got %v %v, want allow", dec.Effect, err)
					return
				}
				dec, err = p.Decide(context.Background(), access.Subject{ID: "u1"}, "documents:delete", res)
				if err != nil || dec.Effect != access.Deny {
					t.Errorf("got %v %v, want deny", dec.Effect, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
