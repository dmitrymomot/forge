package cookiestore_test

import (
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/cookiestore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/secret"
)

func benchStore(b *testing.B) (*cookiestore.Store, session.Record) {
	b.Helper()
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		b.Fatal(err)
	}
	store, err := cookiestore.New(box)
	if err != nil {
		b.Fatal(err)
	}
	rec := session.Record{
		ID:        id.NewUUID(),
		UserID:    "user-1",
		Data:      []byte(`{"cart":["sku-1","sku-2","sku-3"],"theme":"dark"}`),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	return store, rec
}

func BenchmarkSave(b *testing.B) {
	store, rec := benchStore(b)
	ctx := b.Context()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.Save(ctx, "", rec); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoad(b *testing.B) {
	store, rec := benchStore(b)
	ctx := b.Context()
	token, err := store.Save(ctx, "", rec)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.Load(ctx, token); err != nil {
			b.Fatal(err)
		}
	}
}
