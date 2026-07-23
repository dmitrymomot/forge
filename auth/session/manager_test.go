package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// noTouchStore implements Store but not Toucher. It embeds the Store
// interface, not the concrete *MemoryStore, so only Load/Create/Update/Delete
// promote — embedding the concrete type would also promote MemoryStore's
// Touch method and defeat the point of this type.
type noTouchStore struct{ session.Store }

func TestNewRequiresStore(t *testing.T) {
	if _, err := session.New(session.DefaultConfig()); !errors.Is(err, session.ErrNoStore) {
		t.Fatalf("New without WithStore = %v, want ErrNoStore", err)
	}
}

func TestNewAcceptsTouchWithoutToucher(t *testing.T) {
	if _, err := session.New(session.DefaultConfig(),
		session.WithStore(noTouchStore{session.NewMemoryStore()}),
		session.WithTouch(time.Minute),
	); err != nil {
		t.Fatalf("New = %v — without a Toucher the refresh falls back to a full save, not a boot error", err)
	}
}

// TestLoadEnforcesTightenedAbsoluteLifetime pins that a config change applies
// on the very next load: a record persisted under a generous MaxTTL, read by a
// manager whose MaxTTL now puts CreatedAt+cap in the past, is expired — not
// admitted one more time on the strength of its stored deadline.
func TestLoadEnforcesTightenedAbsoluteLifetime(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	store := session.NewMemoryStore()

	generous, err := session.New(session.DefaultConfig(), session.WithStore(store),
		session.WithClock(clk), session.WithIdle(4*time.Hour), session.WithMaxTTL(0))
	if err != nil {
		t.Fatalf("New (generous): %v", err)
	}
	sess := generous.Start()
	if err := generous.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tightened, err := session.New(session.DefaultConfig(), session.WithStore(store),
		session.WithClock(clk), session.WithIdle(time.Hour), session.WithMaxTTL(time.Hour))
	if err != nil {
		t.Fatalf("New (tightened): %v", err)
	}

	// 90 minutes in: the stored deadline (start+4h) is alive, but the tightened
	// cap (CreatedAt+1h) is already behind us.
	clk.Advance(90 * time.Minute)
	if _, err := tightened.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load under the tightened MaxTTL = %v, want ErrExpired", err)
	}
	if _, err := store.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("a record expired under the current policy must be deleted, not left for one more request")
	}
}

func TestStartCostsNoStorage(t *testing.T) {
	store := session.NewMemoryStore()
	mgr, err := session.New(session.DefaultConfig(), session.WithStore(store))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sess := mgr.Start()
	if _, err := store.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Start must not write a row; the row is minted on first save")
	}
}

func TestSaveThenLoad(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()
	nsCart.Set(sess, cartData{Items: []string{"a"}})

	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if sess.IsNew() {
		t.Fatal("a saved session must no longer report IsNew")
	}

	got, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID() != sess.ID() {
		t.Fatalf("Load returned session %v, want %v", got.ID(), sess.ID())
	}
}

func TestLoadExpiredIsErrExpired(t *testing.T) {
	clk := clock.NewMock(time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))
	mgr := newTestManager(t, session.WithClock(clk), session.WithIdle(time.Hour), session.WithMaxTTL(0))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	clk.Advance(2 * time.Hour)
	if _, err := mgr.Load(t.Context(), sess.Token()); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load after idle timeout = %v, want ErrExpired", err)
	}
}

func TestAbsoluteLifetimeCapsSliding(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(80*time.Minute))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	capAt := start.Add(80 * time.Minute)

	// 30 minutes in, uncapped sliding would reach start+90m; the cap is
	// start+80m — a genuinely different instant, so this actually proves
	// capping rather than coinciding with the uncapped value.
	clk.Advance(30 * time.Minute)
	loaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.ExpiresAt().Equal(capAt) {
		t.Fatalf("ExpiresAt = %v, want %v (capped by MaxTTL, not %v uncapped)", loaded.ExpiresAt(), capAt, start.Add(90*time.Minute))
	}
	if err := mgr.Save(t.Context(), loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// More real activity, still inside the cap: the deadline must hold
	// steady at capAt even though every request refreshes LastSeenAt.
	clk.Advance(30 * time.Minute) // start+60m
	loaded, err = mgr.Load(t.Context(), loaded.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.ExpiresAt().Equal(capAt) {
		t.Fatalf("ExpiresAt = %v, want %v (continued activity must not push it past the cap)", loaded.ExpiresAt(), capAt)
	}
	if err := mgr.Save(t.Context(), loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Cross the cap boundary: no amount of activity revives it.
	clk.Advance(30 * time.Minute) // start+90m, past capAt
	if _, err := mgr.Load(t.Context(), loaded.Token()); !errors.Is(err, session.ErrExpired) {
		t.Fatalf("Load past the absolute lifetime = %v, want ErrExpired", err)
	}
}

func TestZeroMaxTTLNeverCaps(t *testing.T) {
	start := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewMock(start)
	mgr := newTestManager(t, session.WithClock(clk),
		session.WithIdle(time.Hour), session.WithMaxTTL(0))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Stay active well past any absolute lifetime; sliding alone must keep it alive.
	for range 100 {
		clk.Advance(30 * time.Minute)
		loaded, err := mgr.Load(t.Context(), sess.Token())
		if err != nil {
			t.Fatalf("Load during continuous activity: %v", err)
		}
		if err := mgr.Save(t.Context(), loaded); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
}
