// Package storetest is the conformance suite every session.Store driver runs.
// It exercises the required interface, then each optional capability the store
// claims, skipping the rest.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

// Run executes the full suite against stores produced by newStore.
func Run(t *testing.T, newStore func(*testing.T) session.Store) {
	t.Helper()
	t.Run("LoadMissing", func(t *testing.T) { testLoadMissing(t, newStore(t)) })
	t.Run("CreateLoadDelete", func(t *testing.T) { testCreateLoadDelete(t, newStore(t)) })
	t.Run("CreateReturnsToken", func(t *testing.T) { testCreateReturnsToken(t, newStore(t)) })
	t.Run("CreateExistingFails", func(t *testing.T) { testCreateExistingFails(t, newStore(t)) })
	t.Run("UpdateOverwrites", func(t *testing.T) { testUpdateOverwrites(t, newStore(t)) })
	t.Run("UpdateMissingFails", func(t *testing.T) { testUpdateMissingFails(t, newStore(t)) })
	t.Run("UpdateAfterDeleteFails", func(t *testing.T) { testUpdateAfterDeleteFails(t, newStore(t)) })
	t.Run("DeleteMissingIsNotAnError", func(t *testing.T) { testDeleteMissing(t, newStore(t)) })
	t.Run("Toucher", func(t *testing.T) { testToucher(t, newStore(t)) })
	t.Run("ToucherMonotonic", func(t *testing.T) { testToucherMonotonic(t, newStore(t)) })
	t.Run("ToucherMissing", func(t *testing.T) { testToucherMissing(t, newStore(t)) })
	t.Run("UserIndex", func(t *testing.T) { testUserIndex(t, newStore(t)) })
	t.Run("UserIndexListOrder", func(t *testing.T) { testUserIndexOrder(t, newStore(t)) })
	t.Run("UserIndexScoped", func(t *testing.T) { testUserIndexScoped(t, newStore(t)) })
	t.Run("Expirer", func(t *testing.T) { testExpirer(t, newStore(t)) })
	t.Run("ConcurrentAccess", func(t *testing.T) { testConcurrentAccess(t, newStore(t)) })
}

// rec builds a fully-populated Record for the given user and expiry. Every
// field carries a distinct non-zero value so round-trip assertions can catch
// a driver that drops or mismaps a column. Timestamps are truncated to
// millisecond precision because some backends store timestamps at reduced
// precision (e.g. a DB column with millisecond resolution); truncating here
// keeps a spec-compliant driver from failing on precision it never promised
// to preserve.
func rec(userID string, expiresAt time.Time) session.Record {
	now := time.Now().UTC().Truncate(time.Millisecond)
	return session.Record{
		ID:          id.NewUUID(),
		UserID:      userID,
		Tenant:      "storetest-tenant",
		CreatedAt:   now,
		LastSeenAt:  now,
		ExpiresAt:   expiresAt,
		ElevatedAt:  now.Add(-5 * time.Minute),
		IP:          "203.0.113.7",
		UserAgent:   "storetest-agent/1.0",
		Fingerprint: "fp-abc123",
		Payload:     []byte(`{"k":{"v":1}}`),
		Remembered:  true,
	}
}

// recAt is rec with an explicit CreatedAt, for tests that need distinct,
// deliberately out-of-order creation times (e.g. ListByUser ordering).
func recAt(userID string, createdAt, expiresAt time.Time) session.Record {
	r := rec(userID, expiresAt)
	r.CreatedAt = createdAt.UTC().Truncate(time.Millisecond)
	return r
}

func testLoadMissing(t *testing.T, st session.Store) {
	if _, err := st.Load(context.Background(), "nope"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load(missing) = %v, want ErrNotFound", err)
	}
}

func testCreateLoadDelete(t *testing.T, st session.Store) {
	ctx := context.Background()
	want := rec("u1", time.Now().Add(time.Hour))

	tok, err := st.Create(ctx, "tok-1", want)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != want.ID || got.UserID != want.UserID {
		t.Fatalf("Load returned %+v, want id=%v user=%q", got, want.ID, want.UserID)
	}
	if got.Tenant != want.Tenant {
		t.Fatalf("Tenant round trip: got %q want %q", got.Tenant, want.Tenant)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("payload round trip: got %s want %s", got.Payload, want.Payload)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("CreatedAt round trip: got %v want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("ExpiresAt round trip: got %v want %v", got.ExpiresAt, want.ExpiresAt)
	}
	if !got.LastSeenAt.Equal(want.LastSeenAt) {
		t.Fatalf("LastSeenAt round trip: got %v want %v", got.LastSeenAt, want.LastSeenAt)
	}
	if !got.ElevatedAt.Equal(want.ElevatedAt) {
		t.Fatalf("ElevatedAt round trip: got %v want %v", got.ElevatedAt, want.ElevatedAt)
	}
	if got.IP != want.IP {
		t.Fatalf("IP round trip: got %q want %q", got.IP, want.IP)
	}
	if got.UserAgent != want.UserAgent {
		t.Fatalf("UserAgent round trip: got %q want %q", got.UserAgent, want.UserAgent)
	}
	if got.Fingerprint != want.Fingerprint {
		t.Fatalf("Fingerprint round trip: got %q want %q", got.Fingerprint, want.Fingerprint)
	}
	if got.Remembered != want.Remembered {
		t.Fatalf("Remembered round trip: got %v want %v", got.Remembered, want.Remembered)
	}

	if err := st.Delete(ctx, tok); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Load(ctx, tok); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
}

func testCreateReturnsToken(t *testing.T, st session.Store) {
	tok, err := st.Create(context.Background(), "tok-2", rec("u1", time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tok == "" {
		t.Fatal("Create must return the token the client should present next")
	}
}

// testCreateExistingFails pins the insert-only half of the contract: Create
// against an already-stored token must fail with ErrExists and must not
// disturb the existing record.
func testCreateExistingFails(t *testing.T, st session.Store) {
	ctx := context.Background()
	const tok = "tok-create-dup"

	first := rec("u1", time.Now().Add(time.Hour))
	if _, err := st.Create(ctx, tok, first); err != nil {
		t.Fatalf("Create (first): %v", err)
	}

	second := rec("u1", time.Now().Add(2*time.Hour))
	if _, err := st.Create(ctx, tok, second); !errors.Is(err, session.ErrExists) {
		t.Fatalf("Create (second, same token) = %v, want ErrExists", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != first.ID {
		t.Fatalf("failed Create must not disturb the stored record: got ID %v, want %v", got.ID, first.ID)
	}
}

// testUpdateOverwrites updates an existing record with different contents and
// confirms the rewrite wins with no duplicate left behind.
func testUpdateOverwrites(t *testing.T, st session.Store) {
	ctx := context.Background()
	const tok = "tok-overwrite"

	first := rec("u1", time.Now().Add(time.Hour))
	if _, err := st.Create(ctx, tok, first); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := rec("u1", time.Now().Add(2*time.Hour))
	second.Payload = []byte(`{"k":{"v":2}}`)
	gotTok, err := st.Update(ctx, tok, second)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := st.Load(ctx, gotTok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ID != second.ID {
		t.Fatalf("Update did not win: got ID %v, want %v", got.ID, second.ID)
	}
	if string(got.Payload) != string(second.Payload) {
		t.Fatalf("Update did not win: got payload %s, want %s", got.Payload, second.Payload)
	}
	if !got.ExpiresAt.Equal(second.ExpiresAt) {
		t.Fatalf("Update did not win: got ExpiresAt %v, want %v", got.ExpiresAt, second.ExpiresAt)
	}

	ix, ok := st.(session.UserIndex)
	if !ok {
		return
	}
	list, err := ix.ListByUser(ctx, "", "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Update must not duplicate: ListByUser returned %d records, want 1", len(list))
	}
	if list[0].ID != second.ID {
		t.Fatalf("ListByUser returned a stale record: got %v, want %v", list[0].ID, second.ID)
	}
}

// testUpdateMissingFails pins the update-only half of the contract: Update
// must never insert.
func testUpdateMissingFails(t *testing.T, st session.Store) {
	ctx := context.Background()
	if _, err := st.Update(ctx, "tok-never-created", rec("u1", time.Now().Add(time.Hour))); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Update(missing) = %v, want ErrNotFound", err)
	}
	if _, err := st.Load(ctx, "tok-never-created"); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Update(missing) must not insert a record")
	}
}

// testUpdateAfterDeleteFails is the revocation-is-terminal guarantee: a
// snapshot that lost a race with Delete cannot resurrect the record by
// committing after it. An upserting driver fails here.
func testUpdateAfterDeleteFails(t *testing.T, st session.Store) {
	ctx := context.Background()
	const tok = "tok-revoked"

	r := rec("u1", time.Now().Add(time.Hour))
	if _, err := st.Create(ctx, tok, r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Delete(ctx, tok); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	stale := r
	stale.ExpiresAt = time.Now().Add(2 * time.Hour)
	if _, err := st.Update(ctx, tok, stale); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Update(deleted) = %v, want ErrNotFound — a stale snapshot must not resurrect a revoked session", err)
	}
	if _, err := st.Load(ctx, tok); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("Update(deleted) must not recreate the record")
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
	tok, err := st.Create(ctx, "tok-3", r)
	if err != nil {
		t.Fatalf("Create: %v", err)
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
	if !got.LastSeenAt.Equal(newSeen) {
		t.Fatalf("Touch did not move LastSeenAt: got %v want %v", got.LastSeenAt, newSeen)
	}
	if string(got.Payload) != string(r.Payload) {
		t.Fatal("Touch must be metadata-only and must not disturb the payload")
	}
}

// testToucherMonotonic pins the Toucher doc's monotonicity requirement: a
// racing request's older timestamps must never move LastSeenAt or ExpiresAt
// backward.
func testToucherMonotonic(t *testing.T, st session.Store) {
	tc, ok := st.(session.Toucher)
	if !ok {
		t.Skip("store does not implement Toucher")
	}
	ctx := context.Background()
	r := rec("u1", time.Now().Add(time.Hour))
	tok, err := st.Create(ctx, "tok-monotonic", r)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ahead := r.LastSeenAt.Add(10 * time.Minute)
	aheadExp := r.ExpiresAt.Add(10 * time.Minute)
	if err := tc.Touch(ctx, tok, ahead, aheadExp); err != nil {
		t.Fatalf("Touch (forward): %v", err)
	}
	if err := tc.Touch(ctx, tok, r.LastSeenAt, r.ExpiresAt); err != nil {
		t.Fatalf("Touch (stale): %v", err)
	}

	got, err := st.Load(ctx, tok)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.LastSeenAt.Equal(ahead) {
		t.Fatalf("stale Touch moved LastSeenAt backward: got %v want %v", got.LastSeenAt, ahead)
	}
	if !got.ExpiresAt.Equal(aheadExp) {
		t.Fatalf("stale Touch moved ExpiresAt backward: got %v want %v", got.ExpiresAt, aheadExp)
	}
}

func testToucherMissing(t *testing.T, st session.Store) {
	tc, ok := st.(session.Toucher)
	if !ok {
		t.Skip("store does not implement Toucher")
	}
	now := time.Now()
	if err := tc.Touch(context.Background(), "tok-never-created", now, now.Add(time.Hour)); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Touch(missing) = %v, want ErrNotFound", err)
	}
}

func testUserIndex(t *testing.T, st session.Store) {
	ix, ok := st.(session.UserIndex)
	if !ok {
		t.Skip("store does not implement UserIndex")
	}
	ctx := context.Background()
	a, b, c := rec("u1", time.Now().Add(time.Hour)), rec("u1", time.Now().Add(time.Hour)), rec("u1", time.Now().Add(time.Hour))
	other := rec("u2", time.Now().Add(time.Hour))
	for tok, r := range map[string]session.Record{"ta": a, "tb": b, "tc": c, "to": other} {
		if _, err := st.Create(ctx, tok, r); err != nil {
			t.Fatalf("Create %s: %v", tok, err)
		}
	}

	list, err := ix.ListByUser(ctx, "", "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByUser returned %d records, want 3", len(list))
	}

	if err := ix.DeleteOne(ctx, "", "u2", a.ID); err != nil {
		t.Fatalf("DeleteOne cross-user: %v", err)
	}
	if list, _ := ix.ListByUser(ctx, "", "u1"); len(list) != 3 {
		t.Fatal("DeleteOne must refuse a session that belongs to another user")
	}

	// Positive case: delete one of u1's own sessions as u1 and confirm the
	// specific record is actually gone, not just that DeleteOne returned nil.
	if err := ix.DeleteOne(ctx, "", "u1", a.ID); err != nil {
		t.Fatalf("DeleteOne own session: %v", err)
	}
	if _, err := st.Load(ctx, "ta"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("DeleteOne did not remove the record: Load = %v, want ErrNotFound", err)
	}
	list, err = ix.ListByUser(ctx, "", "u1")
	if err != nil {
		t.Fatalf("ListByUser after DeleteOne: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("DeleteOne must remove exactly one record: got %d, want 2", len(list))
	}
	for _, r := range list {
		if r.ID == a.ID {
			t.Fatal("DeleteOne left the deleted record reachable through ListByUser")
		}
	}

	if err := ix.DeleteByUser(ctx, "", "u1", b.ID); err != nil {
		t.Fatalf("DeleteByUser: %v", err)
	}
	list, err = ix.ListByUser(ctx, "", "u1")
	if err != nil {
		t.Fatalf("ListByUser after keep-list delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("keep-list not honored: %+v", list)
	}
	if others, _ := ix.ListByUser(ctx, "", "u2"); len(others) != 1 {
		t.Fatal("DeleteByUser must not touch another user's sessions")
	}
}

// testUserIndexOrder verifies ListByUser's documented newest-first ordering
// with three or more records, saved out of chronological order, so a driver
// that merely returns insertion order (or its reverse) fails the check.
func testUserIndexOrder(t *testing.T, st session.Store) {
	ix, ok := st.(session.UserIndex)
	if !ok {
		t.Skip("store does not implement UserIndex")
	}
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)
	oldest := recAt("u1", base.Add(-2*time.Hour), base.Add(time.Hour))
	middle := recAt("u1", base.Add(-time.Hour), base.Add(time.Hour))
	newest := recAt("u1", base, base.Add(time.Hour))

	// Save deliberately out of chronological order: middle, newest, oldest.
	if _, err := st.Create(ctx, "order-middle", middle); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, "order-newest", newest); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, "order-oldest", oldest); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := ix.ListByUser(ctx, "", "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByUser returned %d records, want 3", len(list))
	}
	if list[0].ID != newest.ID || list[1].ID != middle.ID || list[2].ID != oldest.ID {
		t.Fatalf("ListByUser must return newest-first: got CreatedAt order %v, %v, %v",
			list[0].CreatedAt, list[1].CreatedAt, list[2].CreatedAt)
	}
}

// testUserIndexScoped pins the tenant-filter contract every UserIndex driver
// must honor: "" matches any tenant (mirroring apikey's Filter.Tenant), a
// non-empty tenant confines both the read and the deletes, and a same-user
// record owned by another tenant is never reachable through a scoped call.
func testUserIndexScoped(t *testing.T, st session.Store) {
	ix, ok := st.(session.UserIndex)
	if !ok {
		t.Skip("store does not implement UserIndex")
	}
	ctx := context.Background()
	const user = "scoped-u"
	t1a := rec(user, time.Now().Add(time.Hour))
	t1a.Tenant = "t1"
	t1b := rec(user, time.Now().Add(time.Hour))
	t1b.Tenant = "t1"
	t2 := rec(user, time.Now().Add(time.Hour))
	t2.Tenant = "t2"

	for tok, r := range map[string]session.Record{"scoped-t1-a": t1a, "scoped-t1-b": t1b, "scoped-t2": t2} {
		if _, err := st.Create(ctx, tok, r); err != nil {
			t.Fatalf("Create %s: %v", tok, err)
		}
	}

	if list, err := ix.ListByUser(ctx, "t1", user); err != nil {
		t.Fatalf("ListByUser(t1): %v", err)
	} else if len(list) != 2 {
		t.Fatalf("ListByUser(t1) returned %d records, want 2", len(list))
	}

	if list, err := ix.ListByUser(ctx, "t2", user); err != nil {
		t.Fatalf("ListByUser(t2): %v", err)
	} else if len(list) != 1 {
		t.Fatalf("ListByUser(t2) returned %d records, want 1", len(list))
	}

	if list, err := ix.ListByUser(ctx, "", user); err != nil {
		t.Fatalf("ListByUser(\"\"): %v", err)
	} else if len(list) != 3 {
		t.Fatalf("ListByUser(\"\") must see every tenant, got %d, want 3", len(list))
	}

	if err := ix.DeleteOne(ctx, "t1", user, t2.ID); err != nil {
		t.Fatalf("DeleteOne(t1, t2's session id): %v", err)
	}
	if _, err := st.Load(ctx, "scoped-t2"); err != nil {
		t.Fatalf("DeleteOne(t1) must be a no-op against a t2 session, even for the same user: %v", err)
	}

	if err := ix.DeleteByUser(ctx, "t1", user); err != nil {
		t.Fatalf("DeleteByUser(t1): %v", err)
	}
	if _, err := st.Load(ctx, "scoped-t1-a"); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("DeleteByUser(t1) must remove t1's sessions")
	}
	if _, err := st.Load(ctx, "scoped-t1-b"); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("DeleteByUser(t1) must remove t1's sessions")
	}
	if _, err := st.Load(ctx, "scoped-t2"); err != nil {
		t.Fatalf("DeleteByUser(t1) must not remove the t2 session: %v", err)
	}
}

// testExpirer pins the two boundary decisions documented on Expirer: a zero
// ExpiresAt never expires, and the now boundary is inclusive (a record
// expiring exactly at now is reaped).
func testExpirer(t *testing.T, st session.Store) {
	ex, ok := st.(session.Expirer)
	if !ok {
		t.Skip("store does not implement Expirer")
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	past := rec("u1", now.Add(-time.Hour))
	future := rec("u1", now.Add(time.Hour))
	onBoundary := rec("u1", now)      // expires exactly at now: inclusive boundary, must be reaped
	forever := rec("u1", time.Time{}) // zero ExpiresAt: never expires, must never be reaped

	if _, err := st.Create(ctx, "expired", past); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, "live", future); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, "boundary", onBoundary); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, "forever", forever); err != nil {
		t.Fatalf("Create: %v", err)
	}

	n, err := ex.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteExpired removed %d, want 2 (past + on-boundary)", n)
	}
	if _, err := st.Load(ctx, "live"); err != nil {
		t.Fatalf("DeleteExpired removed a live session: %v", err)
	}
	if _, err := st.Load(ctx, "boundary"); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("DeleteExpired must treat ExpiresAt == now as expired (inclusive boundary): Load = %v, want ErrNotFound", err)
	}
	if _, err := st.Load(ctx, "forever"); err != nil {
		t.Fatalf("DeleteExpired must never reap a zero ExpiresAt: %v", err)
	}
}

// testConcurrentAccess hammers Save/Load/Delete from several goroutines
// against distinct tokens. Store's doc comment requires implementations be
// safe for concurrent use; this is the suite's only probe of that under
// -race, where a data race or a non-atomic upsert would surface as a failure
// or a race report.
func testConcurrentAccess(t *testing.T, st session.Store) {
	ctx := context.Background()
	const workers = 8
	const iterations = 50

	var wg sync.WaitGroup
	errs := make(chan error, workers*iterations)
	for w := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range iterations {
				tok := fmt.Sprintf("concurrent-%d-%d", worker, i)
				r := rec("u-concurrent", time.Now().Add(time.Hour))
				savedTok, err := st.Create(ctx, tok, r)
				if err != nil {
					errs <- fmt.Errorf("save: %w", err)
					continue
				}
				got, err := st.Load(ctx, savedTok)
				if err != nil {
					errs <- fmt.Errorf("load: %w", err)
					continue
				}
				if got.ID != r.ID {
					errs <- fmt.Errorf("load returned wrong record: got %v want %v", got.ID, r.ID)
					continue
				}
				if err := st.Delete(ctx, savedTok); err != nil {
					errs <- fmt.Errorf("delete: %w", err)
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
