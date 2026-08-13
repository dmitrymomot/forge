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
	cfg, err := apikey.NewConfig(apikey.WithPrefix("sk"))
	if err != nil {
		f.Fatal(err)
	}
	mem := apikey.NewMemoryStore()
	_, plaintext, err := apikey.Create(context.Background(), cfg,
		apikey.CreateParams{Subject: "u1"}, mem.Save)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(plaintext)
	f.Add("")
	f.Add("sk_")
	f.Add("sk_" + strings.Repeat("a", 49))
	f.Add(plaintext[:len(plaintext)-1] + "!")

	f.Fuzz(func(t *testing.T, cred string) {
		identity, err := apikey.Verify(context.Background(), cfg, cred, mem.LoadByHash, mem.Touch)
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
