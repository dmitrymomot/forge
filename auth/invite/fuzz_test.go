package invite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/invite"
)

// FuzzPeek asserts the core property: no input other than the real
// plaintext ever verifies, and no input panics the parser. Peek (not
// Accept) keeps the invite unconsumed across iterations.
func FuzzPeek(f *testing.F) {
	mgr := invite.New(invite.NewMemoryStore())
	_, plaintext, err := mgr.Create(context.Background(), invite.CreateParams{Email: "a@b.com", Tenant: "t1"})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(plaintext)
	f.Add("")
	f.Add("inv_")
	f.Add("inv_" + strings.Repeat("a", 49))
	f.Add(plaintext[:len(plaintext)-1] + "!")

	f.Fuzz(func(t *testing.T, token string) {
		claim, err := mgr.Peek(context.Background(), token)
		if err == nil {
			if token != plaintext {
				t.Fatalf("accepted forged token %q", token)
			}
			if claim.Email != "a@b.com" || claim.Tenant != "t1" {
				t.Fatalf("wrong claim %+v", claim)
			}
		}
	})
}
