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
	var stored apikey.Key
	_, secret, err := apikey.Create(context.Background(), cfg,
		apikey.CreateParams{Subject: "u1"}, captureKey(&stored))
	if err != nil {
		f.Fatal(err)
	}
	plaintext := secret.Expose()
	load := loadsKeyByHash(stored)

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
