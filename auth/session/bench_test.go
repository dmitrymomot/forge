package session_test

import (
	"context"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/resilience/cache"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

type benchData struct {
	Cart  []string `json:"cart,omitempty"`
	Theme string   `json:"theme,omitempty"`
}

func benchSession(b *testing.B, mgr *session.Manager[benchData], ctx context.Context) *session.Session[benchData] {
	b.Helper()
	s := mgr.Start(ctx)
	s.Data = benchData{Cart: []string{"sku-1", "sku-2", "sku-3"}, Theme: "dark"}
	if err := mgr.Save(ctx, s); err != nil {
		b.Fatal(err)
	}
	return s
}

func BenchmarkSave_Memory(b *testing.B) {
	mgr, err := session.New[benchData](session.NewMemoryStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	s := benchSession(b, mgr, ctx)
	b.ReportAllocs()
	for b.Loop() {
		if err := mgr.Save(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoad_Memory(b *testing.B) {
	mgr, err := session.New[benchData](session.NewMemoryStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	s := benchSession(b, mgr, ctx)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Load(ctx, s.Token); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRotate_Memory(b *testing.B) {
	mgr, err := session.New[benchData](session.NewMemoryStore())
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	s := benchSession(b, mgr, ctx)
	b.ReportAllocs()
	for b.Loop() {
		if err := mgr.Rotate(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
}

var benchDigestKey = ctxkey.New[fingerprint.Digest]("bench-digest")

func BenchmarkLoad_MemoryFingerprintStrict(b *testing.B) {
	src := func(ctx context.Context) (fingerprint.Digest, bool) { return benchDigestKey.From(ctx) }
	mgr, err := session.New[benchData](session.NewMemoryStore(),
		session.WithFingerprint(session.Strict), session.WithDigestSource(src))
	if err != nil {
		b.Fatal(err)
	}
	ctx := benchDigestKey.With(b.Context(), fingerprint.Digest{
		Parts:   map[string]string{"ua": "aa", "ip": "bb", "tls": "cc"},
		Hash:    "combined",
		Version: 1,
	})
	s := benchSession(b, mgr, ctx)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := mgr.Load(ctx, s.Token); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSaveLoad_KV(b *testing.B) {
	kvBackend := cache.NewMemoryStore()
	defer func() { _ = kvBackend.Close() }()
	kv, err := session.NewKVStore(kvBackend)
	if err != nil {
		b.Fatal(err)
	}
	mgr, err := session.New[benchData](kv)
	if err != nil {
		b.Fatal(err)
	}
	ctx := b.Context()
	s := benchSession(b, mgr, ctx)
	b.ReportAllocs()
	for b.Loop() {
		if err := mgr.Save(ctx, s); err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.Load(ctx, s.Token); err != nil {
			b.Fatal(err)
		}
	}
}
