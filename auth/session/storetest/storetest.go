// Package storetest is the conformance suite every session.Store driver runs.
// It exercises the required interface, then each optional capability the store
// claims, skipping the rest.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// Run executes the full suite against stores produced by newStore.
func Run(t *testing.T, newStore func(*testing.T) session.Store) {
	t.Helper()
	t.Run("LoadMissing", func(t *testing.T) { testLoadMissing(t, newStore(t)) })
	t.Run("SaveLoadDelete", func(t *testing.T) { testSaveLoadDelete(t, newStore(t)) })
	t.Run("SaveReturnsToken", func(t *testing.T) { testSaveReturnsToken(t, newStore(t)) })
	t.Run("DeleteMissingIsNotAnError", func(t *testing.T) { testDeleteMissing(t, newStore(t)) })
	t.Run("Toucher", func(t *testing.T) { testToucher(t, newStore(t)) })
	t.Run("UserIndex", func(t *testing.T) { testUserIndex(t, newStore(t)) })
	t.Run("Expirer", func(t *testing.T) { testExpirer(t, newStore(t)) })
}

func rec(userID string, expiresAt time.Time) session.Record {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return session.Record{
		ID:         id.NewUUID(),
		UserID:     userID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expiresAt,
		Payload:    []byte(`{"k":{"v":1}}`),
	}
}

func testLoadMissing(t *testing.T, st session.Store) {
	if _, err := st.Load(context.Background(), "nope"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load(missing) = %v, want ErrNotFound", err)
	}
}

func testSaveLoadDelete(t *testing.T, st session.Store) {
	ctx := context.Background()
	want := rec("u1", time.Now().Add(time.Hour))

	tok, err := st.Save(ctx, "tok-1", want)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != want.ID || got.UserID != want.UserID {
		t.Fatalf("Load returned %+v, want id=%v user=%q", got, want.ID, want.UserID)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("payload round trip: got %s want %s", got.Payload, want.Payload)
	}

	if err := st.Delete(ctx, tok); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Load(ctx, tok); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
}

func testSaveReturnsToken(t *testing.T, st session.Store) {
	tok, err := st.Save(context.Background(), "tok-2", rec("u1", time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if tok == "" {
		t.Fatal("Save must return the token the client should present next")
	}
}

func testDeleteMissing(t *testing.T, st session.Store) {
	if err := st.Delete(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Delete(missing) = %v, want nil", err)
	}
}

func testToucher(t *testing.T, st session.Store) {
	tc, ok := st.(session.Toucher)
	if !ok {
		t.Skip("store does not implement Toucher")
	}
	ctx := context.Background()
	r := rec("u1", time.Now().Add(time.Hour))
	tok, err := st.Save(ctx, "tok-3", r)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	newSeen := r.LastSeenAt.Add(10 * time.Minute)
	newExp := r.ExpiresAt.Add(10 * time.Minute)
	if err := tc.Touch(ctx, tok, newSeen, newExp); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.ExpiresAt.Equal(newExp) {
		t.Fatalf("Touch did not move ExpiresAt: got %v want %v", got.ExpiresAt, newExp)
	}
	if string(got.Payload) != string(r.Payload) {
		t.Fatal("Touch must be metadata-only and must not disturb the payload")
	}
}

func testUserIndex(t *testing.T, st session.Store) {
	ix, ok := st.(session.UserIndex)
	if !ok {
		t.Skip("store does not implement UserIndex")
	}
	ctx := context.Background()
	a, b, other := rec("u1", time.Now().Add(time.Hour)), rec("u1", time.Now().Add(time.Hour)), rec("u2", time.Now().Add(time.Hour))
	for tok, r := range map[string]session.Record{"ta": a, "tb": b, "tc": other} {
		if _, err := st.Save(ctx, tok, r); err != nil {
			t.Fatalf("Save %s: %v", tok, err)
		}
	}

	list, err := ix.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByUser returned %d records, want 2", len(list))
	}

	if err := ix.DeleteOne(ctx, "u2", a.ID); err != nil {
		t.Fatalf("DeleteOne cross-user: %v", err)
	}
	if list, _ := ix.ListByUser(ctx, "u1"); len(list) != 2 {
		t.Fatal("DeleteOne must refuse a session that belongs to another user")
	}

	if err := ix.DeleteByUser(ctx, "u1", b.ID); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	list, err = ix.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser after keep-list delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("keep-list not honored: %+v", list)
	}
	if others, _ := ix.ListByUser(ctx, "u2"); len(others) != 1 {
		t.Fatal("DeleteByUser must not touch another user's sessions")
	}
}

func testExpirer(t *testing.T, st session.Store) {
	ex, ok := st.(session.Expirer)
	if !ok {
		t.Skip("store does not implement Expirer")
	}
	ctx := context.Background()
	past := rec("u1", time.Now().Add(-time.Hour))
	future := rec("u1", time.Now().Add(time.Hour))
	if _, err := st.Save(ctx, "expired", past); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := st.Save(ctx, "live", future); err != nil {
		t.Fatalf("Save: %v", err)
	}

	n, err := ex.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteExpired removed %d, want 1", n)
	}
	if _, err := st.Load(ctx, "live"); err != nil {
		t.Fatalf("DeleteExpired removed a live session: %v", err)
	}
}
