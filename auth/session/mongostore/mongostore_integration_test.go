//go:build integration

package mongostore_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/mongostore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/testkit/mongotest"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

// runID keeps re-runs against a persistent server (FORGE_TEST_MONGO_URI)
// collision-free: every process uses its own collection.
var runID = id.NewULID().String()

func newStore(t *testing.T) *mongostore.Store {
	t.Helper()
	client, err := mongodriver.Connect(options.Client().ApplyURI(mongotest.URI(t)))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(ctx)
	})
	s, err := mongostore.New(client.Database("forge_session_test"), mongostore.WithCollection("st_"+runID))
	require.NoError(t, err)
	require.NoError(t, s.EnsureIndexes(context.Background()))
	return s
}

// mkRecord builds a record whose user/scope are unique per call so tests
// sharing the process collection never see each other's rows.
func mkRecord(t *testing.T) session.Record {
	t.Helper()
	uid := id.NewUUID()
	now := time.Now().UTC().Truncate(time.Millisecond) // BSON datetime precision
	return session.Record{
		ID:         uid,
		UserID:     "user-" + uid.String(),
		Scope:      "scope-" + uid.String(),
		IP:         "203.0.113.7",
		UserAgent:  "browser-a",
		Data:       []byte(`{"theme":"dark"}`),
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
		LastSeenAt: now,
	}
}

func newToken() string { return random.URLSafe(32) }

func TestMongo_InvalidConfig(t *testing.T) {
	_, err := mongostore.New(nil)
	require.Error(t, err)
	client, err := mongodriver.Connect(options.Client().ApplyURI(mongotest.URI(t)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	_, err = mongostore.New(client.Database("forge_session_test"), mongostore.WithCollection("bad name!"))
	require.Error(t, err)
}

func TestMongo_SaveLoadRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord(t)
	rec.Fingerprint = fingerprint.Digest{
		Parts:   map[string]string{"ua": "aa", "ip": "bb"},
		Hash:    "combined",
		Version: 1,
	}
	token := newToken()

	returned, err := s.Save(ctx, token, rec)
	require.NoError(t, err)
	assert.Equal(t, token, returned, "server-side stores must not rewrite the token")

	got, err := s.Load(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.UserID, got.UserID)
	assert.Equal(t, rec.Scope, got.Scope)
	assert.Equal(t, rec.Data, got.Data)
	assert.Equal(t, rec.Fingerprint, got.Fingerprint)
	assert.Equal(t, rec.IP, got.IP)
	assert.Equal(t, rec.UserAgent, got.UserAgent)
	// BSON datetimes are millisecond-precision; mkRecord pre-truncates, so
	// exact equality is expected (in UTC).
	assert.True(t, rec.CreatedAt.Equal(got.CreatedAt), "created_at: want %v got %v", rec.CreatedAt, got.CreatedAt)
	assert.True(t, rec.ExpiresAt.Equal(got.ExpiresAt), "expires_at: want %v got %v", rec.ExpiresAt, got.ExpiresAt)
	assert.True(t, rec.LastSeenAt.Equal(got.LastSeenAt), "last_seen_at: want %v got %v", rec.LastSeenAt, got.LastSeenAt)
}

func TestMongo_LoadUnknown(t *testing.T) {
	s := newStore(t)
	_, err := s.Load(context.Background(), newToken())
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestMongo_SaveUpsertsAndFingerprintEmpty(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord(t)
	token := newToken()

	_, err := s.Save(ctx, token, rec)
	require.NoError(t, err)

	rec.UserID = "rebound-" + rec.UserID
	rec.Data = []byte(`{"theme":"light"}`)
	rec.ExpiresAt = rec.ExpiresAt.Add(time.Hour)
	_, err = s.Save(ctx, token, rec)
	require.NoError(t, err, "second save under the same token must upsert")

	got, err := s.Load(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, rec.UserID, got.UserID)
	assert.Equal(t, rec.Data, got.Data)
	assert.Empty(t, got.Fingerprint.Hash, "a zero digest must round-trip as zero")
}

func TestMongo_Delete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	token := newToken()
	_, err := s.Save(ctx, token, mkRecord(t))
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, token))
	_, err = s.Load(ctx, token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	require.NoError(t, s.Delete(ctx, token), "deleting an absent token is a no-op")
}

func TestMongo_UserIndex(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	scope := "scope-" + id.NewUUID().String()
	user := "user-" + id.NewUUID().String()
	gen := id.NewGenerator(id.WithMonotonic())

	var ids [3]id.UUID
	for i := range 3 {
		rec := mkRecord(t)
		rec.ID = gen.UUID()
		rec.Scope, rec.UserID = scope, user
		ids[i] = rec.ID
		_, err := s.Save(ctx, newToken(), rec)
		require.NoError(t, err)
	}
	// Same user in a sibling scope must stay invisible.
	other := mkRecord(t)
	other.UserID = user
	otherToken := newToken()
	_, err := s.Save(ctx, otherToken, other)
	require.NoError(t, err)

	list, err := s.ListByUser(ctx, scope, user)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, ids[2], list[0].ID, "newest first")
	assert.Equal(t, ids[0], list[2].ID)

	// Keep-list: "log out other devices" preserves exactly the kept id.
	require.NoError(t, s.DeleteByUser(ctx, scope, user, ids[2]))
	list, err = s.ListByUser(ctx, scope, user)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, ids[2], list[0].ID)

	require.NoError(t, s.DeleteByUser(ctx, scope, user))
	list, err = s.ListByUser(ctx, scope, user)
	require.NoError(t, err)
	assert.Empty(t, list)
	_, err = s.Load(ctx, otherToken)
	require.NoError(t, err, "sibling-scope session must survive DeleteByUser")

	require.NoError(t, s.DeleteByUser(ctx, scope, user), "deleting a user with no sessions is a no-op")
}

func TestMongo_DeleteOne(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	rec := mkRecord(t)
	token := newToken()
	_, err := s.Save(ctx, token, rec)
	require.NoError(t, err)

	// Wrong user binding revokes nothing (IDOR guard).
	require.NoError(t, s.DeleteOne(ctx, rec.Scope, "someone-else", rec.ID))
	_, err = s.Load(ctx, token)
	require.NoError(t, err)

	require.NoError(t, s.DeleteOne(ctx, rec.Scope, rec.UserID, rec.ID))
	_, err = s.Load(ctx, token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	require.NoError(t, s.DeleteOne(ctx, rec.Scope, rec.UserID, rec.ID), "revoking an absent session is a no-op")
}

// TestMongo_ManagerEndToEnd runs the full Manager lifecycle against MongoDB,
// including the device-management verbs.
func TestMongo_ManagerEndToEnd(t *testing.T) {
	type data struct {
		Theme string `json:"theme,omitempty"`
	}
	mgr, err := session.New[data](newStore(t))
	require.NoError(t, err)
	ctx := context.Background()

	s := mgr.Start(ctx)
	s.Data.Theme = "dark"
	require.NoError(t, mgr.Save(ctx, s))
	user := "user-" + s.ID.String()
	require.NoError(t, mgr.Authenticate(ctx, s, user))

	otherDevice := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, otherDevice, user))

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, "dark", got.Data.Theme)

	list, err := mgr.ListUserSessions(ctx, user)
	require.NoError(t, err)
	require.Len(t, list, 2)

	require.NoError(t, mgr.LogoutOthers(ctx, s))
	_, err = mgr.Load(ctx, otherDevice.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = mgr.Load(ctx, s.Token)
	require.NoError(t, err)

	require.NoError(t, mgr.Destroy(ctx, s))
	_, err = mgr.Load(ctx, got.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
}
