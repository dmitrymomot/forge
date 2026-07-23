package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// failingSaveStore fails the Nth save, to prove rollback.
type failingSaveStore struct {
	*session.MemoryStore
	failOn int
	calls  int
}

func (f *failingSaveStore) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	f.calls++
	if f.calls == f.failOn {
		return "", errors.New("store unavailable")
	}
	return f.MemoryStore.Save(ctx, token, rec)
}

func TestAuthenticateRotatesTokenAndPreservesIdentity(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	oldToken, oldID, oldCreated := sess.Token(), sess.ID(), sess.CreatedAt()

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if sess.Token() == oldToken {
		t.Fatal("Authenticate must rotate the token — session fixation defense")
	}
	if sess.ID() != oldID {
		t.Fatal("Authenticate must preserve the session ID across rotation")
	}
	if !sess.CreatedAt().Equal(oldCreated) {
		t.Fatal("Authenticate must preserve CreatedAt across rotation")
	}
	if sess.UserID() != "u1" {
		t.Fatalf("UserID() = %q, want u1", sess.UserID())
	}
	if _, err := mgr.Load(t.Context(), oldToken); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the pre-rotation token must stop working")
	}
}

func TestAuthenticatePreservesPayload(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"guest-item"}})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cart, err := nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0] != "guest-item" {
		t.Fatalf("anonymous cart lost on login: %+v", cart)
	}
}

func TestAuthenticateRollsBackTokenOnFailedSave(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	oldToken := sess.Token()

	store.failOn = store.calls + 1
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err == nil {
		t.Fatal("Authenticate must surface the store failure")
	}

	if sess.Token() != oldToken {
		t.Fatal("a failed save must roll the token back, or the client is holding a credential no record answers to")
	}
	if sess.UserID() != "" {
		t.Fatal("a failed save must roll the user binding back too")
	}
	if _, err := mgr.Load(t.Context(), oldToken); err != nil {
		t.Fatalf("the original session must still load after a failed Authenticate: %v", err)
	}
}

func TestFailedAuthenticateKeepsPendingPayloadForRetry(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"guest-item"}})

	store.failOn = store.calls + 1
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err == nil {
		t.Fatal("Authenticate must surface the store failure")
	}

	// The rollback undid UserID/token, but the cart write was never
	// persisted anywhere — it must still be pending, not silently dropped.
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("retry Save: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cart, err := nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(cart.Items) != 1 || cart.Items[0] != "guest-item" {
		t.Fatalf("pending namespace write lost after a failed Authenticate: %+v", cart)
	}
}

func TestRememberSelectsTheRememberDeadlines(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(0),
		session.WithRememberIdle(30*24*time.Hour), session.WithRememberMaxTTL(0))

	plain := mgr.Start()
	if err := mgr.Authenticate(t.Context(), plain, "u1", session.Remember(false)); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if plain.Remembered() {
		t.Fatal("Remember(false) must not mark the session remembered")
	}
	if want := start.Add(time.Hour); !plain.ExpiresAt().Equal(want) {
		t.Fatalf("plain ExpiresAt = %v, want %v", plain.ExpiresAt(), want)
	}

	remembered := mgr.Start()
	if err := mgr.Authenticate(t.Context(), remembered, "u2", session.Remember(true)); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !remembered.Remembered() {
		t.Fatal("Remember(true) must mark the session remembered")
	}
	if want := start.Add(30 * 24 * time.Hour); !remembered.ExpiresAt().Equal(want) {
		t.Fatalf("remembered ExpiresAt = %v, want %v", remembered.ExpiresAt(), want)
	}
}

func TestDestroyRemovesTheRecord(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	token := sess.Token()

	if err := mgr.Destroy(t.Context(), sess); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := mgr.Load(t.Context(), token); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load after Destroy = %v, want ErrNotFound", err)
	}
}

func TestElevationSurvivesRotation(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := mgr.Rotate(t.Context(), sess); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !sess.ElevatedWithin(time.Minute) {
		t.Fatal("rotation must preserve ElevatedAt")
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.ElevatedWithin(time.Minute) {
		t.Fatal("rotation must persist ElevatedAt, not just hold it in memory")
	}
}

func TestElevateStampsFreshly(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk))

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	clk.Advance(20 * time.Minute)
	if sess.ElevatedWithin(10 * time.Minute) {
		t.Fatal("elevation must have gone stale")
	}
	if err := mgr.Elevate(t.Context(), sess); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	if !sess.ElevatedWithin(10 * time.Minute) {
		t.Fatal("Elevate must refresh the stamp")
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.ElevatedWithin(10 * time.Minute) {
		t.Fatal("Elevate must persist the refreshed stamp, not just hold it in memory")
	}
}

func TestRebindReplacesPinnedMetadata(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Rebind(t.Context(), sess, session.Bind{IP: "203.0.113.4", UserAgent: "Chrome", Fingerprint: "fp1"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if sess.IP() != "203.0.113.4" || sess.UserAgent() != "Chrome" || sess.Fingerprint() != "fp1" {
		t.Fatalf("Rebind did not apply: ip=%q ua=%q fp=%q", sess.IP(), sess.UserAgent(), sess.Fingerprint())
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.IP() != "203.0.113.4" || reloaded.UserAgent() != "Chrome" || reloaded.Fingerprint() != "fp1" {
		t.Fatalf("Rebind did not persist: ip=%q ua=%q fp=%q", reloaded.IP(), reloaded.UserAgent(), reloaded.Fingerprint())
	}
}
