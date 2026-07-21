package cookiestore_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/cookiestore"
	"github.com/dmitrymomot/forge/crypto/secret"
)

// FuzzLoad pins the only acceptable outcomes for arbitrary tokens: a clean
// session.ErrNotFound or — for the rare forgery-sized inputs — never a panic
// or a successfully decoded record.
func FuzzLoad(f *testing.F) {
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		f.Fatal(err)
	}
	store, err := cookiestore.New(box)
	if err != nil {
		f.Fatal(err)
	}
	valid, err := store.Save(f.Context(), "", session.Record{Data: []byte(`{"a":1}`)})
	if err != nil {
		f.Fatal(err)
	}

	f.Add("")
	f.Add("AAAA")
	f.Add("!!!not-base64!!!")
	f.Add(valid)
	f.Add(valid[:len(valid)-1])

	f.Fuzz(func(t *testing.T, token string) {
		rec, err := store.Load(t.Context(), token)
		if err != nil {
			if !errors.Is(err, session.ErrNotFound) {
				t.Fatalf("unexpected error class: %v", err)
			}
			return
		}
		// A successful decode cryptographically implies the AEAD tag
		// verified, i.e. this key sealed the token — in this or an earlier
		// process (the fuzz corpus replays tokens across runs), possibly
		// spelled with different base64 trailing bits. All records this
		// harness ever seals carry exactly this payload; anything else means
		// the store decoded a record it never sealed.
		if !bytes.Equal(rec.Data, []byte(`{"a":1}`)) || rec.UserID != "" || !rec.ID.IsZero() {
			t.Fatalf("forged token decoded into a record: %q -> %+v", token, rec)
		}
	})
}
