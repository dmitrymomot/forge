package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// failingSaveStore fails the Nth write (Create or Update), to prove rollback.
type failingSaveStore struct {
	*session.MemoryStore
	failOn int
	calls  int
}

func (f *failingSaveStore) Create(ctx context.Context, token string, rec session.Record) (string, error) {
	f.calls++
	if f.calls == f.failOn {
		return "", errors.New("store unavailable")
	}
	return f.MemoryStore.Create(ctx, token, rec)
}

func (f *failingSaveStore) Update(ctx context.Context, token string, rec session.Record) (string, error) {
	f.calls++
	if f.calls == f.failOn {
		return "", errors.New("store unavailable")
	}
	return f.MemoryStore.Update(ctx, token, rec)
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

func TestRotateRollsBackTheWholeRecordOnFailedSave(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	oldToken, oldExpires := sess.Token(), sess.ExpiresAt()

	store.failOn = store.calls + 1
	if err := mgr.Rotate(t.Context(), sess); err == nil {
		t.Fatal("Rotate must surface the store failure")
	}

	if sess.Token() != oldToken {
		t.Fatal("a failed rotation must roll the token back")
	}
	if !sess.ExpiresAt().Equal(oldExpires) {
		t.Fatal("a failed rotation must not leave a deadline the store never committed")
	}
	if _, err := mgr.Load(t.Context(), oldToken); err != nil {
		t.Fatalf("the original session must still load after a failed Rotate: %v", err)
	}
}

func TestElevateRollsBackTheWholeRecordOnFailedSave(t *testing.T) {
	store := &failingSaveStore{MemoryStore: session.NewMemoryStore()}
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	oldToken, oldElevated, oldExpires := sess.Token(), sess.ElevatedAt(), sess.ExpiresAt()

	store.failOn = store.calls + 1
	if err := mgr.Elevate(t.Context(), sess); err == nil {
		t.Fatal("Elevate must surface the store failure")
	}

	if sess.Token() != oldToken {
		t.Fatal("a failed elevation must roll the token back")
	}
	if !sess.ElevatedAt().Equal(oldElevated) {
		t.Fatal("a failed elevation must not leave a fresher stamp than the store holds")
	}
	if !sess.ExpiresAt().Equal(oldExpires) {
		t.Fatal("a failed elevation must not leave a deadline the store never committed")
	}
	if _, err := mgr.Load(t.Context(), oldToken); err != nil {
		t.Fatalf("the original session must still load after a failed Elevate: %v", err)
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

func TestAuthenticateRejectsSwitchingUsers(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"user-a-item"}})
	if err := mgr.Authenticate(t.Context(), sess, "user-a"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	// Same user re-authenticating is fine (e.g. password re-entry).
	if err := mgr.Authenticate(t.Context(), sess, "user-a"); err != nil {
		t.Fatalf("same-user re-authentication: %v", err)
	}
	tokenAfterA := sess.Token()

	// A different user must be rejected: the payload still carries user-a's data.
	if err := mgr.Authenticate(t.Context(), sess, "user-b"); !errors.Is(err, session.ErrUserMismatch) {
		t.Fatalf("Authenticate(user-b) on user-a's session = %v, want ErrUserMismatch", err)
	}
	if sess.UserID() != "user-a" {
		t.Fatalf("rejected switch must leave the binding intact: UserID = %q", sess.UserID())
	}
	if sess.Token() != tokenAfterA {
		t.Fatal("a rejected switch must not rotate the credential")
	}
}

func TestElevateRotatesTheToken(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	stolen := sess.Token()

	if err := mgr.Elevate(t.Context(), sess); err != nil {
		t.Fatalf("Elevate: %v", err)
	}
	if sess.Token() == stolen {
		t.Fatal("Elevate must rotate the token — a credential copied before step-up must not inherit the elevation")
	}
	if _, err := mgr.Load(t.Context(), stolen); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the pre-elevation token must stop working")
	}
	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reloaded.ElevatedWithin(time.Minute) {
		t.Fatal("the rotated record must carry the fresh elevation stamp")
	}
}

func TestElevateRejectsAnonymousSessions(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Elevate(t.Context(), sess); !errors.Is(err, session.ErrAnonymous) {
		t.Fatalf("Elevate on an anonymous session = %v, want ErrAnonymous — there is no identity to re-prove", err)
	}
}

func TestDestroyDeauthenticatesTheInMemorySession(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if err := mgr.Destroy(t.Context(), sess); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if sess.Authenticated() {
		t.Fatal("code later in the request must not keep authorizing a destroyed session")
	}
	if err := mgr.Save(t.Context(), sess); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Save after Destroy = %v, want ErrNotFound — a destroyed session must not be resurrectable", err)
	}
}

// TestStaleSnapshotCannotResurrectADestroyedSession is the manager-level
// revocation-is-terminal proof: request A holds a loaded snapshot, request B
// destroys the session, then A commits. The upsert contract this replaces
// would recreate the record with a fresh deadline.
func TestStaleSnapshotCannotResurrectADestroyedSession(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	if err := mgr.Authenticate(t.Context(), sess, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	token := sess.Token()

	snapshotA, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load (request A): %v", err)
	}
	snapshotB, err := mgr.Load(t.Context(), token)
	if err != nil {
		t.Fatalf("Load (request B): %v", err)
	}

	if err := mgr.Destroy(t.Context(), snapshotB); err != nil {
		t.Fatalf("Destroy (request B): %v", err)
	}

	nsCart.Set(snapshotA, cartData{Items: []string{"stale"}})
	if err := mgr.Save(t.Context(), snapshotA); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("stale Save after Destroy = %v, want ErrNotFound", err)
	}
	if _, err := mgr.Load(t.Context(), token); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the destroyed record must stay destroyed")
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
