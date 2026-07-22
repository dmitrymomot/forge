package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

// bareStore implements only Store — no UserIndex, no Expirer.
type bareStore struct{ inner *session.MemoryStore }

func (b bareStore) Load(ctx context.Context, tok string) (session.Record, error) {
	return b.inner.Load(ctx, tok)
}
func (b bareStore) Save(ctx context.Context, tok string, r session.Record) (string, error) {
	return b.inner.Save(ctx, tok, r)
}
func (b bareStore) Delete(ctx context.Context, tok string) error { return b.inner.Delete(ctx, tok) }

func TestMissingCapabilityIsErrUnsupported(t *testing.T) {
	mgr, err := session.New(session.DefaultConfig(),
		session.WithStore(bareStore{session.NewMemoryStore()}),
		session.WithTouch(0),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := mgr.ListByUser(t.Context(), "u1"); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("ListByUser = %v, want ErrUnsupported", err)
	}
	if err := mgr.Revoke(t.Context(), "u1", id.NewUUID()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("Revoke = %v, want ErrUnsupported", err)
	}
	if _, err := mgr.DeleteExpired(t.Context()); !errors.Is(err, session.ErrUnsupported) {
		t.Fatalf("DeleteExpired = %v, want ErrUnsupported", err)
	}
}

func TestLogoutOthersKeepsTheCurrentSession(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	current := mgr.Start()
	if err := mgr.Authenticate(ctx, current, "u1"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	for range 3 {
		other := mgr.Start()
		if err := mgr.Authenticate(ctx, other, "u1"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}

	// A second user's sessions must be untouched by u1's logout-others: a
	// Manager-level wiring regression (e.g. dropping the keep-list) that also
	// nuked another user's sessions would go uncaught with only one user seeded.
	bystander := mgr.Start()
	if err := mgr.Authenticate(ctx, bystander, "u2"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	list, err := mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("expected 4 sessions before logout-others, got %d", len(list))
	}

	if err := mgr.LogoutOthers(ctx, current); err != nil {
		t.Fatalf("LogoutOthers: %v", err)
	}

	list, err = mgr.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != current.ID() {
		t.Fatalf("LogoutOthers left %d sessions, want only the current one", len(list))
	}
	if _, err := mgr.Load(ctx, current.Token()); err != nil {
		t.Fatalf("the current session must still load after LogoutOthers: %v", err)
	}
	if _, err := mgr.Load(ctx, bystander.Token()); err != nil {
		t.Fatalf("LogoutOthers for u1 must not touch u2's session: %v", err)
	}
}

func TestRevokeIsUserBound(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	victim := mgr.Start()
	if err := mgr.Authenticate(ctx, victim, "victim"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	// An attacker knows the victim's session id and tries to revoke it as themselves.
	if err := mgr.Revoke(ctx, "attacker", victim.ID()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); err != nil {
		t.Fatalf("Revoke must refuse a session belonging to another user: %v", err)
	}

	// The owner can revoke it.
	if err := mgr.Revoke(ctx, "victim", victim.ID()); err != nil {
		t.Fatalf("Revoke by owner: %v", err)
	}
	if _, err := mgr.Load(ctx, victim.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("the owner's revoke must remove the session")
	}
}

func TestReaperServiceName(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Minute)
	if svc.Name() == "" {
		t.Fatal("a supervisor.Service must report a name")
	}
}

func TestReaperStopsOnContextCancel(t *testing.T) {
	mgr := newTestManager(t)
	svc := session.Reaper(mgr, time.Hour)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reaper did not stop within 2s of cancellation")
	}
}

func TestReaperDeletesExpiredRecordsOnTick(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := t.Context()

	const expiredTok, liveTok = "reaper-expired-tok", "reaper-live-tok"
	if _, err := store.Save(ctx, expiredTok, session.Record{
		ID:        id.NewUUID(),
		CreatedAt: time.Now().Add(-time.Hour),
		ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed expired record: %v", err)
	}
	if _, err := store.Save(ctx, liveTok, session.Record{
		ID:        id.NewUUID(),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed live record: %v", err)
	}

	svc := session.Reaper(mgr, 20*time.Millisecond)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- svc.Run(runCtx) }()

	// Poll for the effect of a real tick instead of sleeping a fixed duration
	// and hoping: this is what actually proves the ticker branch runs.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := store.Load(ctx, expiredTok); errors.Is(err, session.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a tick did not reap the expired record within 3s")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := store.Load(ctx, liveTok); err != nil {
		t.Fatalf("the reaper must not delete a live record: %v", err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Reaper did not stop within 2s of cancellation")
	}
}

func TestDeleteExpiredUsesManagerClockAndBoundary(t *testing.T) {
	// Far in the future so a break that swaps in the real wall clock cannot
	// coincidentally reap (or fail to reap) the right records by luck.
	start := time.Date(3000, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store), session.WithClock(clk))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := t.Context()

	const beforeTok, afterTok = "boundary-before-tok", "boundary-after-tok"
	if _, err := store.Save(ctx, beforeTok, session.Record{
		ID: id.NewUUID(), CreatedAt: start, ExpiresAt: start.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := store.Save(ctx, afterTok, session.Record{
		ID: id.NewUUID(), CreatedAt: start, ExpiresAt: start.Add(time.Minute),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := mgr.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpired = %d, want 1", n)
	}

	if _, err := store.Load(ctx, beforeTok); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("a record expiring before the manager's clock must be reaped")
	}
	if _, err := store.Load(ctx, afterTok); err != nil {
		t.Fatalf("a record expiring after the manager's clock must survive: %v", err)
	}
}

func TestAnonymousGuardsRejectEmptyUserID(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	if _, err := mgr.ListByUser(ctx, ""); !errors.Is(err, session.ErrAnonymous) {
		t.Fatalf("ListByUser(\"\") = %v, want ErrAnonymous", err)
	}
	if err := mgr.Revoke(ctx, "", id.NewUUID()); !errors.Is(err, session.ErrAnonymous) {
		t.Fatalf("Revoke(\"\", ...) = %v, want ErrAnonymous", err)
	}
	if err := mgr.DeleteByUser(ctx, ""); !errors.Is(err, session.ErrAnonymous) {
		t.Fatalf("DeleteByUser(\"\") = %v, want ErrAnonymous", err)
	}

	anon := mgr.Start()
	if err := mgr.LogoutOthers(ctx, anon); !errors.Is(err, session.ErrAnonymous) {
		t.Fatalf("LogoutOthers on an anonymous session = %v, want ErrAnonymous", err)
	}
}

func TestDeleteByUserRemovesOnlyThatUsersSessions(t *testing.T) {
	mgr := newTestManager(t)
	ctx := t.Context()

	for range 3 {
		s := mgr.Start()
		if err := mgr.Authenticate(ctx, s, "victim"); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}
	bystander := mgr.Start()
	if err := mgr.Authenticate(ctx, bystander, "bystander"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	if err := mgr.DeleteByUser(ctx, "victim"); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}

	list, err := mgr.ListByUser(ctx, "victim")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("DeleteByUser left %d sessions for victim, want 0", len(list))
	}

	list, err = mgr.ListByUser(ctx, "bystander")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("DeleteByUser(victim) left %d sessions for bystander, want 1 untouched", len(list))
	}
	if _, err := mgr.Load(ctx, bystander.Token()); err != nil {
		t.Fatalf("another user's session must still load after DeleteByUser: %v", err)
	}
}
