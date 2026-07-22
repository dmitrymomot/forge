package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
)

// noTouchStore implements Store but not Toucher. It embeds the Store
// interface, not the concrete *MemoryStore, so only Load/Save/Delete
// promote — embedding the concrete type would also promote MemoryStore's
// Touch method and defeat the point of this type.
type noTouchStore struct{ session.Store }

func TestNewRequiresStore(t *testing.T) {
	if _, err := session.New(session.DefaultConfig()); !errors.Is(err, session.ErrNoStore) {
		t.Fatalf("New without WithStore = %v, want ErrNoStore", err)
	}
}

func TestNewRejectsTouchWithoutToucher(t *testing.T) {
	_, err := session.New(session.DefaultConfig(),
		session.WithStore(noTouchStore{session.NewMemoryStore()}),
		session.WithTouch(time.Minute),
	)
	if !errors.Is(err, session.ErrTouchUnsupported) {
		t.Fatalf("New = %v, want ErrTouchUnsupported — a configured option whose capability is missing is a boot error", err)
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
		session.WithIdle(time.Hour), session.WithMaxTTL(90*time.Minute))

	sess := mgr.Start()
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 30 minutes in, sliding would reach start+90m; the cap is start+90m too.
	clk.Advance(30 * time.Minute)
	loaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := mgr.Save(t.Context(), loaded); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := start.Add(90 * time.Minute); !loaded.ExpiresAt().Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v (capped by MaxTTL)", loaded.ExpiresAt(), want)
	}

	// Past the cap, no amount of activity revives it.
	clk.Advance(61 * time.Minute)
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
