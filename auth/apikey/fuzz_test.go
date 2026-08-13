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
	cfg := mustConfig(f, apikey.WithPrefix("sk"))
	k, plaintext := issueKey(f, cfg, apikey.CreateParams{Subject: "u1"})
	load := loadsKeyByHash(k)

	f.Add(plaintext)
	f.Add("")
	f.Add("sk_")
	f.Add("sk_" + strings.Repeat("a", 49))
	f.Add(plaintext[:len(plaintext)-1] + "!")

	f.Fuzz(func(t *testing.T, credential string) {
		identity, err := apikey.Verify(context.Background(), cfg, credential, load, nil)
		if err == nil {
			if credential != plaintext {
				t.Fatalf("accepted forged credential %q", credential)
			}
			if identity.Subject != "u1" {
				t.Fatalf("wrong subject %q", identity.Subject)
			}
		}
	})
}
