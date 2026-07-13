package apikey_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/apikey"
)

// FuzzVerify asserts the core property: no input other than the real
// plaintext ever authenticates, and no input panics the parser.
func FuzzVerify(f *testing.F) {
	store := apikey.NewMemoryStore()
	mgr := apikey.New(store, apikey.WithPrefix("sk"))
	_, plaintext, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	if err != nil {
		f.Fatal(err)
	}

	f.Add(plaintext)
	f.Add("")
	f.Add("sk_")
	f.Add("sk_" + strings.Repeat("a", 49))
	f.Add(plaintext[:len(plaintext)-1] + "!")

	f.Fuzz(func(t *testing.T, cred string) {
		identity, err := mgr.Verify(context.Background(), cred)
		if err == nil {
			if cred != plaintext {
				t.Fatalf("accepted forged credential %q", cred)
			}
			if identity.Subject != "u1" {
				t.Fatalf("wrong subject %q", identity.Subject)
			}
		}
	})
}
