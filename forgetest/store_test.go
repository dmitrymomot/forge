package forgetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/id"
)

func TestMemoryStore_CreateAndGetByTokenHash(t *testing.T) {
	t.Parallel()

	t.Run("round-trip stores and retrieves session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "hash123",
			UserID:       &userID,
			IP:           "192.168.1.1",
			UserAgent:    "test-agent",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         map[string]any{"key": "value"},
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		retrieved, err := store.GetByTokenHash(ctx, "hash123")
		require.NoError(t, err)
		require.Equal(t, session.ID, retrieved.ID)
		require.Equal(t, session.TokenHash, retrieved.TokenHash)
		require.Equal(t, *session.UserID, *retrieved.UserID)
		require.Equal(t, session.IP, retrieved.IP)
		require.Equal(t, session.UserAgent, retrieved.UserAgent)
		require.Equal(t, "value", retrieved.Data["key"])
	})

	t.Run("anonymous session without UserID", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "anon-hash",
			UserID:       nil,
			IP:           "10.0.0.1",
			UserAgent:    "browser",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		retrieved, err := store.GetByTokenHash(ctx, "anon-hash")
		require.NoError(t, err)
		require.Nil(t, retrieved.UserID)
		require.Equal(t, session.ID, retrieved.ID)
	})
}

func TestMemoryStore_GetByTokenHash_NotFound(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrSessionNotFound for missing hash", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		_, err := store.GetByTokenHash(ctx, "nonexistent")
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionNotFound))
	})
}

func TestMemoryStore_GetByTokenHash_Expired(t *testing.T) {
	t.Parallel()

	t.Run("returns ErrSessionExpired for expired session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "expired-hash",
			CreatedAt:    time.Now().Add(-2 * time.Hour),
			LastActiveAt: time.Now().Add(-2 * time.Hour),
			ExpiresAt:    time.Now().Add(-time.Hour), // Already expired
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		_, err = store.GetByTokenHash(ctx, "expired-hash")
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionExpired))
	})
}

func TestMemoryStore_Update(t *testing.T) {
	t.Parallel()

	t.Run("changes session data", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "hash1",
			UserID:       &userID,
			IP:           "1.2.3.4",
			UserAgent:    "old-agent",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         map[string]any{"counter": 1},
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Update session
		session.UserAgent = "new-agent"
		session.Data["counter"] = 2
		session.LastActiveAt = time.Now().Add(time.Minute)

		err = store.Update(ctx, session)
		require.NoError(t, err)

		// Verify changes
		retrieved, err := store.GetByTokenHash(ctx, "hash1")
		require.NoError(t, err)
		require.Equal(t, "new-agent", retrieved.UserAgent)
		require.Equal(t, 2, retrieved.Data["counter"])
	})

	t.Run("returns ErrSessionNotFound for nonexistent session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:        "nonexistent-id",
			TokenHash: "hash",
		}

		err := store.Update(ctx, session)
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionNotFound))
	})
}

func TestMemoryStore_Update_TokenRotation(t *testing.T) {
	t.Parallel()

	t.Run("handles token rotation correctly", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "old-hash",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Rotate token
		session.TokenHash = "new-hash"
		err = store.Update(ctx, session)
		require.NoError(t, err)

		// Old hash should not work
		_, err = store.GetByTokenHash(ctx, "old-hash")
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionNotFound))

		// New hash should work
		retrieved, err := store.GetByTokenHash(ctx, "new-hash")
		require.NoError(t, err)
		require.Equal(t, session.ID, retrieved.ID)
	})
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("removes session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "delete-hash",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Verify it exists
		_, err = store.GetByTokenHash(ctx, "delete-hash")
		require.NoError(t, err)

		// Delete it
		err = store.Delete(ctx, session.ID)
		require.NoError(t, err)

		// Verify it's gone
		_, err = store.GetByTokenHash(ctx, "delete-hash")
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionNotFound))
	})

	t.Run("no error when deleting nonexistent session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		err := store.Delete(ctx, "nonexistent-id")
		require.NoError(t, err)
	})
}

func TestMemoryStore_ListByUserID(t *testing.T) {
	t.Parallel()

	t.Run("returns correct sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		user1 := id.NewULID()
		user2 := id.NewULID()

		// Create sessions for user1
		for range 3 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user1,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		// Create sessions for user2
		for range 2 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user2,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		// List user1 sessions
		sessions, err := store.ListByUserID(ctx, user1)
		require.NoError(t, err)
		require.Len(t, sessions, 3)
		for _, s := range sessions {
			require.Equal(t, user1, *s.UserID)
		}

		// List user2 sessions
		sessions, err = store.ListByUserID(ctx, user2)
		require.NoError(t, err)
		require.Len(t, sessions, 2)
	})

	t.Run("returns empty slice for user with no sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		sessions, err := store.ListByUserID(ctx, "no-sessions-user")
		require.NoError(t, err)
		require.Empty(t, sessions)
	})

	t.Run("excludes anonymous sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		// Create anonymous session
		anonSession := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       nil,
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		err := store.Create(ctx, anonSession)
		require.NoError(t, err)

		// Should not appear in any user's sessions
		sessions, err := store.ListByUserID(ctx, "any-user")
		require.NoError(t, err)
		require.Empty(t, sessions)
	})
}

func TestMemoryStore_CountByUserID(t *testing.T) {
	t.Parallel()

	t.Run("returns correct count", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()

		// Create 5 sessions
		for range 5 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &userID,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		count, err := store.CountByUserID(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, 5, count)
	})

	t.Run("returns zero for user with no sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		count, err := store.CountByUserID(ctx, "no-sessions")
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestMemoryStore_DeleteByUserID(t *testing.T) {
	t.Parallel()

	t.Run("removes all user sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		user1 := id.NewULID()
		user2 := id.NewULID()

		// Create sessions for both users
		for range 3 {
			session1 := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user1,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session1)
			require.NoError(t, err)

			session2 := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user2,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err = store.Create(ctx, session2)
			require.NoError(t, err)
		}

		// Delete user1 sessions
		err := store.DeleteByUserID(ctx, user1)
		require.NoError(t, err)

		// Verify user1 sessions are gone
		count, err := store.CountByUserID(ctx, user1)
		require.NoError(t, err)
		require.Equal(t, 0, count)

		// Verify user2 sessions remain
		count, err = store.CountByUserID(ctx, user2)
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("no error when deleting nonexistent user", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		err := store.DeleteByUserID(ctx, "nonexistent-user")
		require.NoError(t, err)
	})
}

func TestMemoryStore_DeleteByUserIDExcept(t *testing.T) {
	t.Parallel()

	t.Run("keeps excepted session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		var sessionIDs []string

		// Create 4 sessions
		for range 4 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &userID,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
			sessionIDs = append(sessionIDs, session.ID)
		}

		// Keep the second session
		keepID := sessionIDs[1]
		err := store.DeleteByUserIDExcept(ctx, userID, keepID)
		require.NoError(t, err)

		// Verify only one session remains
		count, err := store.CountByUserID(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, 1, count)

		// Verify it's the correct session
		sessions, err := store.ListByUserID(ctx, userID)
		require.NoError(t, err)
		require.Len(t, sessions, 1)
		require.Equal(t, keepID, sessions[0].ID)
	})

	t.Run("does not affect other users", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		user1 := id.NewULID()
		user2 := id.NewULID()

		// Create sessions for user1
		for range 3 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user1,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		// Create sessions for user2
		keepSession := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       &user2,
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		err := store.Create(ctx, keepSession)
		require.NoError(t, err)

		for range 2 {
			session := &forge.Session{
				ID:           id.NewULID(),
				TokenHash:    id.NewULID(),
				UserID:       &user2,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		// Delete user2 sessions except one
		err = store.DeleteByUserIDExcept(ctx, user2, keepSession.ID)
		require.NoError(t, err)

		// Verify user1 unaffected
		count, err := store.CountByUserID(ctx, user1)
		require.NoError(t, err)
		require.Equal(t, 3, count)

		// Verify user2 has only one session
		count, err = store.CountByUserID(ctx, user2)
		require.NoError(t, err)
		require.Equal(t, 1, count)
	})
}

func TestMemoryStore_DeleteOldestByUserID(t *testing.T) {
	t.Parallel()

	t.Run("removes oldest by LastActiveAt", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		baseTime := time.Now()

		// Create sessions with different LastActiveAt times
		oldest := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       &userID,
			CreatedAt:    baseTime,
			LastActiveAt: baseTime.Add(-3 * time.Hour), // Oldest
			ExpiresAt:    baseTime.Add(time.Hour),
		}
		err := store.Create(ctx, oldest)
		require.NoError(t, err)

		middle := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       &userID,
			CreatedAt:    baseTime,
			LastActiveAt: baseTime.Add(-1 * time.Hour),
			ExpiresAt:    baseTime.Add(time.Hour),
		}
		err = store.Create(ctx, middle)
		require.NoError(t, err)

		newest := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       &userID,
			CreatedAt:    baseTime,
			LastActiveAt: baseTime,
			ExpiresAt:    baseTime.Add(time.Hour),
		}
		err = store.Create(ctx, newest)
		require.NoError(t, err)

		// Delete oldest
		err = store.DeleteOldestByUserID(ctx, userID)
		require.NoError(t, err)

		// Verify count
		count, err := store.CountByUserID(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, 2, count)

		// Verify oldest is gone
		_, ok := store.GetByID(oldest.ID)
		require.False(t, ok)

		// Verify others remain
		_, ok = store.GetByID(middle.ID)
		require.True(t, ok)
		_, ok = store.GetByID(newest.ID)
		require.True(t, ok)
	})

	t.Run("no error when user has no sessions", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		err := store.DeleteOldestByUserID(ctx, "no-sessions")
		require.NoError(t, err)
	})

	t.Run("handles single session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			UserID:       &userID,
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		err := store.Create(ctx, session)
		require.NoError(t, err)

		err = store.DeleteOldestByUserID(ctx, userID)
		require.NoError(t, err)

		count, err := store.CountByUserID(ctx, userID)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

func TestMemoryStore_Touch(t *testing.T) {
	t.Parallel()

	t.Run("updates LastActiveAt", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		originalTime := time.Now().Add(-time.Hour)
		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			CreatedAt:    originalTime,
			LastActiveAt: originalTime,
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Touch the session
		newTime := time.Now()
		err = store.Touch(ctx, session.ID, newTime)
		require.NoError(t, err)

		// Verify LastActiveAt was updated
		retrieved, ok := store.GetByID(session.ID)
		require.True(t, ok)
		require.True(t, retrieved.LastActiveAt.After(originalTime))
		require.WithinDuration(t, newTime, retrieved.LastActiveAt, time.Second)
	})

	t.Run("returns ErrSessionNotFound for nonexistent session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		err := store.Touch(ctx, "nonexistent-id", time.Now())
		require.Error(t, err)
		require.True(t, errors.Is(err, forge.ErrSessionNotFound))
	})
}

func TestMemoryStore_GetByID(t *testing.T) {
	t.Parallel()

	t.Run("test helper works", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         map[string]any{"test": "data"},
		}
		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Get by ID
		retrieved, ok := store.GetByID(session.ID)
		require.True(t, ok)
		require.Equal(t, session.ID, retrieved.ID)
		require.Equal(t, "data", retrieved.Data["test"])
	})

	t.Run("returns false for nonexistent session", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()

		_, ok := store.GetByID("nonexistent-id")
		require.False(t, ok)
	})
}

func TestMemoryStore_Count(t *testing.T) {
	t.Parallel()

	t.Run("test helper works", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		require.Equal(t, 0, store.Count())

		// Create 3 sessions
		var sessionIDs []string
		for range 3 {
			sessionID := id.NewULID()
			sessionIDs = append(sessionIDs, sessionID)
			session := &forge.Session{
				ID:           sessionID,
				TokenHash:    id.NewULID(),
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		require.Equal(t, 3, store.Count())

		// Delete one
		err := store.Delete(ctx, sessionIDs[0])
		require.NoError(t, err)

		require.Equal(t, 2, store.Count())
	})
}

func TestMemoryStore_DeepCopy(t *testing.T) {
	t.Parallel()

	t.Run("modifying returned session does not affect stored one", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		userID := id.NewULID()
		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    "original-hash",
			UserID:       &userID,
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         map[string]any{"counter": 0},
		}
		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Get and modify
		retrieved, err := store.GetByTokenHash(ctx, "original-hash")
		require.NoError(t, err)

		retrieved.Data["counter"] = 999
		*retrieved.UserID = "modified-user-id"
		retrieved.TokenHash = "modified-hash"

		// Get again and verify original values
		retrieved2, err := store.GetByTokenHash(ctx, "original-hash")
		require.NoError(t, err)
		require.Equal(t, 0, retrieved2.Data["counter"])
		require.Equal(t, userID, *retrieved2.UserID)
		require.Equal(t, "original-hash", retrieved2.TokenHash)
	})

	t.Run("modifying Data map does not affect store", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		session := &forge.Session{
			ID:           id.NewULID(),
			TokenHash:    id.NewULID(),
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         map[string]any{"key1": "value1"},
		}
		err := store.Create(ctx, session)
		require.NoError(t, err)

		// Get and add to Data map
		retrieved, ok := store.GetByID(session.ID)
		require.True(t, ok)
		retrieved.Data["key2"] = "value2"

		// Verify original only has key1
		retrieved2, ok := store.GetByID(session.ID)
		require.True(t, ok)
		require.Len(t, retrieved2.Data, 1)
		require.Equal(t, "value1", retrieved2.Data["key1"])
		require.Nil(t, retrieved2.Data["key2"])
	})
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	t.Run("multiple goroutines doing Create and GetByTokenHash", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		const numGoroutines = 10
		const opsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for g := range numGoroutines {
			go func(goroutineID int) {
				defer wg.Done()

				for i := range opsPerGoroutine {
					sessionID := id.NewULID()
					tokenHash := id.NewULID()

					session := &forge.Session{
						ID:           sessionID,
						TokenHash:    tokenHash,
						CreatedAt:    time.Now(),
						LastActiveAt: time.Now(),
						ExpiresAt:    time.Now().Add(time.Hour),
						Data:         map[string]any{"goroutine": goroutineID, "op": i},
					}

					err := store.Create(ctx, session)
					require.NoError(t, err)

					retrieved, err := store.GetByTokenHash(ctx, tokenHash)
					require.NoError(t, err)
					require.Equal(t, sessionID, retrieved.ID)
					require.Equal(t, goroutineID, retrieved.Data["goroutine"])
					require.Equal(t, i, retrieved.Data["op"])
				}
			}(g)
		}

		wg.Wait()

		// Verify total count
		expectedCount := numGoroutines * opsPerGoroutine
		require.Equal(t, expectedCount, store.Count())
	})

	t.Run("concurrent reads and writes", func(t *testing.T) {
		t.Parallel()

		store := newMemoryStore()
		ctx := context.Background()

		// Create initial sessions
		const numSessions = 10
		sessionIDs := make([]string, numSessions)
		tokenHashes := make([]string, numSessions)

		for i := range numSessions {
			sessionID := id.NewULID()
			tokenHash := id.NewULID()
			sessionIDs[i] = sessionID
			tokenHashes[i] = tokenHash

			session := &forge.Session{
				ID:           sessionID,
				TokenHash:    tokenHash,
				CreatedAt:    time.Now(),
				LastActiveAt: time.Now(),
				ExpiresAt:    time.Now().Add(time.Hour),
				Data:         map[string]any{"counter": 0},
			}
			err := store.Create(ctx, session)
			require.NoError(t, err)
		}

		var wg sync.WaitGroup

		// Start readers
		for range 5 {
			wg.Go(func() {
				for i := range 100 {
					hash := tokenHashes[i%numSessions]
					_, _ = store.GetByTokenHash(ctx, hash)
				}
			})
		}

		// Start writers
		for range 5 {
			wg.Go(func() {
				for i := range 100 {
					sid := sessionIDs[i%numSessions]
					_ = store.Touch(ctx, sid, time.Now())
				}
			})
		}

		wg.Wait()

		// Verify all sessions still exist
		require.Equal(t, numSessions, store.Count())
	})
}
