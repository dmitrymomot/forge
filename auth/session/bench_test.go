package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

var benchCart = session.NewNamespace[cartData]("bench.cart")

func BenchmarkNoOpRequest(b *testing.B) {
	mgr := benchManager(b)
	seed := seedSession(b, mgr)
	mw := session.Middleware(mgr, session.WithTransport(headerTransport{}))
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Test-Token", seed.Token())

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func BenchmarkNamespaceGet(b *testing.B) {
	mgr := benchManager(b)
	sess := seedSessionWithCart(b, mgr)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := benchCart.Get(sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNamespaceSetAndEncode(b *testing.B) {
	mgr := benchManager(b)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		sess := mgr.Start()
		benchCart.Set(sess, cartData{Items: []string{"a", "b", "c"}})
		if err := mgr.Save(b.Context(), sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnknownNamespacePassthrough(b *testing.B) {
	mgr := benchManager(b)
	sess := seedSessionWithForeignKeys(b, mgr)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchCart.Set(sess, cartData{Items: []string{"x"}})
		if err := mgr.Save(b.Context(), sess); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommitWithRotation(b *testing.B) {
	mgr := benchManager(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		sess := mgr.Start()
		if err := mgr.Authenticate(b.Context(), sess, "u1"); err != nil {
			b.Fatal(err)
		}
		_ = i
	}
}

func benchManager(b *testing.B) *session.Manager {
	b.Helper()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(session.NewMemoryStore()))
	if err != nil {
		b.Fatalf("session.New: %v", err)
	}
	return mgr
}

func seedSession(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := mgr.Start()
	if err := mgr.Authenticate(b.Context(), sess, "bench-user"); err != nil {
		b.Fatalf("Authenticate: %v", err)
	}
	return sess
}

func seedSessionWithCart(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := seedSession(b, mgr)
	benchCart.Set(sess, cartData{Items: []string{"a", "b", "c"}})
	if err := mgr.Save(b.Context(), sess); err != nil {
		b.Fatalf("Save: %v", err)
	}
	reloaded, err := mgr.Load(b.Context(), sess.Token())
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	return reloaded
}

// seedSessionWithForeignKeys builds a payload carrying namespaces this process
// never declared, so the benchmark measures raw passthrough on save.
func seedSessionWithForeignKeys(b *testing.B, mgr *session.Manager) *session.Session {
	b.Helper()
	sess := seedSession(b, mgr)
	for _, ns := range []*session.Namespace[map[string]string]{foreignA, foreignB, foreignC} {
		ns.Set(sess, map[string]string{"k1": "v1", "k2": "v2"})
	}
	if err := mgr.Save(b.Context(), sess); err != nil {
		b.Fatalf("Save: %v", err)
	}
	reloaded, err := mgr.Load(b.Context(), sess.Token())
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	return reloaded
}

var (
	foreignA = session.NewNamespace[map[string]string]("bench.foreign.a")
	foreignB = session.NewNamespace[map[string]string]("bench.foreign.b")
	foreignC = session.NewNamespace[map[string]string]("bench.foreign.c")
)
