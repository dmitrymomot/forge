package internal_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
)

func TestSession_ValueOperations(t *testing.T) {
	t.Parallel()

	t.Run("SetValue stores value and marks dirty", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		require.True(t, sess.IsDirty(), "new session starts dirty")
		require.True(t, sess.IsNew(), "new session is marked as new")

		sess.ClearDirty() // Clear initial dirty flag
		require.False(t, sess.IsDirty())

		sess.SetValue("user_pref", "dark_mode")

		require.True(t, sess.IsDirty())
		val, ok := sess.GetValue("user_pref")
		require.True(t, ok)
		require.Equal(t, "dark_mode", val)
	})

	t.Run("GetValue returns false for missing key", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		val, ok := sess.GetValue("nonexistent")

		require.False(t, ok)
		require.Nil(t, val)
	})

	t.Run("DeleteValue marks dirty only if key exists", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		// Delete non-existent key should not mark dirty
		sess.DeleteValue("nonexistent")
		require.False(t, sess.IsDirty())

		// Set a value
		sess.SetValue("temp", "value")
		sess.ClearDirty()

		// Delete existing key should mark dirty
		sess.DeleteValue("temp")
		require.True(t, sess.IsDirty())
		_, ok := sess.GetValue("temp")
		require.False(t, ok)
	})

	t.Run("multiple value operations track dirty state", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		sess.SetValue("key1", "val1")
		sess.SetValue("key2", "val2")
		require.True(t, sess.IsDirty())

		sess.ClearDirty()
		require.False(t, sess.IsDirty())

		sess.DeleteValue("key1")
		require.True(t, sess.IsDirty())
	})
}

func TestSession_StateFlags(t *testing.T) {
	t.Parallel()

	t.Run("IsNew returns true for new session", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		require.True(t, sess.IsNew())

		sess.ClearNew()
		require.False(t, sess.IsNew())
	})

	t.Run("IsExpired detects expired session", func(t *testing.T) {
		t.Parallel()

		pastTime := time.Now().Add(-time.Hour)
		sess := internal.NewSession("test-token", pastTime)

		require.True(t, sess.IsExpired())
	})

	t.Run("IsExpired returns false for valid session", func(t *testing.T) {
		t.Parallel()

		futureTime := time.Now().Add(time.Hour)
		sess := internal.NewSession("test-token", futureTime)

		require.False(t, sess.IsExpired())
	})

	t.Run("IsAuthenticated returns false for anonymous session", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		require.False(t, sess.IsAuthenticated())
		require.Nil(t, sess.UserID)
	})

	t.Run("IsAuthenticated returns true when UserID set", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		userID := "user-123"
		sess.UserID = &userID

		require.True(t, sess.IsAuthenticated())
	})

	t.Run("IsAuthenticated returns false for empty UserID", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		emptyID := ""
		sess.UserID = &emptyID

		require.False(t, sess.IsAuthenticated())
	})
}

func TestSession_DirtyTracking(t *testing.T) {
	t.Parallel()

	t.Run("MarkDirty sets dirty flag", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		sess.MarkDirty()
		require.True(t, sess.IsDirty())
	})

	t.Run("ClearDirty removes dirty flag", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("test-token", time.Now().Add(time.Hour))
		sess.MarkDirty()
		require.True(t, sess.IsDirty())

		sess.ClearDirty()
		require.False(t, sess.IsDirty())
	})
}

func TestNewSession(t *testing.T) {
	t.Parallel()

	t.Run("creates session with hashed token", func(t *testing.T) {
		t.Parallel()

		token := "test-token-12345"
		expiresAt := time.Now().Add(24 * time.Hour)

		sess := internal.NewSession(token, expiresAt)

		require.NotEmpty(t, sess.ID)
		require.NotEmpty(t, sess.TokenHash)
		require.NotNil(t, sess.Data)
		require.True(t, sess.IsNew())
		require.True(t, sess.IsDirty())
		require.Equal(t, expiresAt, sess.ExpiresAt)
	})

	t.Run("token hash is deterministic SHA-256", func(t *testing.T) {
		t.Parallel()

		token := "consistent-token"

		sess1 := internal.NewSession(token, time.Now().Add(time.Hour))
		sess2 := internal.NewSession(token, time.Now().Add(time.Hour))

		// Same token should produce same hash
		require.Equal(t, sess1.TokenHash, sess2.TokenHash)

		// Verify it's actually SHA-256 hashed
		h := sha256.Sum256([]byte(token))
		expectedHash := base64.URLEncoding.EncodeToString(h[:])
		require.Equal(t, expectedHash, sess1.TokenHash)
	})

	t.Run("session timestamps are set", func(t *testing.T) {
		t.Parallel()

		before := time.Now()
		sess := internal.NewSession("token", time.Now().Add(time.Hour))
		after := time.Now()

		require.True(t, sess.CreatedAt.After(before) || sess.CreatedAt.Equal(before))
		require.True(t, sess.CreatedAt.Before(after) || sess.CreatedAt.Equal(after))
		require.Equal(t, sess.CreatedAt, sess.LastActiveAt)
	})

	t.Run("Data map is initialized", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("token", time.Now().Add(time.Hour))

		// Should be able to set values without nil panic
		sess.SetValue("test", "value")
		val, ok := sess.GetValue("test")
		require.True(t, ok)
		require.Equal(t, "value", val)
	})
}

func TestSession_SecurityProperties(t *testing.T) {
	t.Parallel()

	t.Run("different tokens produce different hashes", func(t *testing.T) {
		t.Parallel()

		sess1 := internal.NewSession("token-one", time.Now().Add(time.Hour))
		sess2 := internal.NewSession("token-two", time.Now().Add(time.Hour))

		require.NotEqual(t, sess1.TokenHash, sess2.TokenHash)
	})

	t.Run("token hash is base64 URL-encoded SHA-256", func(t *testing.T) {
		t.Parallel()

		token := "security-test-token"
		sess := internal.NewSession(token, time.Now().Add(time.Hour))

		// Should be valid base64 URL encoding
		decoded, err := base64.URLEncoding.DecodeString(sess.TokenHash)
		require.NoError(t, err)

		// SHA-256 produces 32 bytes
		require.Len(t, decoded, 32)
	})
}

func TestSession_Metadata(t *testing.T) {
	t.Parallel()

	t.Run("session has unique ID", func(t *testing.T) {
		t.Parallel()

		sess1 := internal.NewSession("token1", time.Now().Add(time.Hour))
		sess2 := internal.NewSession("token2", time.Now().Add(time.Hour))

		require.NotEmpty(t, sess1.ID)
		require.NotEmpty(t, sess2.ID)
		require.NotEqual(t, sess1.ID, sess2.ID)
	})

	t.Run("session expiration is respected", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days
		sess := internal.NewSession("token", expiresAt)

		require.Equal(t, expiresAt, sess.ExpiresAt)
		require.False(t, sess.IsExpired())
	})
}

func TestSession_ValueTypes(t *testing.T) {
	t.Parallel()

	t.Run("stores and retrieves various value types", func(t *testing.T) {
		t.Parallel()

		sess := internal.NewSession("token", time.Now().Add(time.Hour))

		// String
		sess.SetValue("name", "Alice")
		name, ok := sess.GetValue("name")
		require.True(t, ok)
		require.Equal(t, "Alice", name)

		// Integer
		sess.SetValue("count", 42)
		count, ok := sess.GetValue("count")
		require.True(t, ok)
		require.Equal(t, 42, count)

		// Boolean
		sess.SetValue("active", true)
		active, ok := sess.GetValue("active")
		require.True(t, ok)
		require.Equal(t, true, active)

		// Slice
		sess.SetValue("items", []string{"a", "b", "c"})
		items, ok := sess.GetValue("items")
		require.True(t, ok)
		require.Equal(t, []string{"a", "b", "c"}, items)

		// Map
		sess.SetValue("config", map[string]int{"max": 100})
		config, ok := sess.GetValue("config")
		require.True(t, ok)
		require.Equal(t, map[string]int{"max": 100}, config)
	})
}

func TestSession_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("nil Data map is initialized on SetValue", func(t *testing.T) {
		t.Parallel()

		sess := &internal.Session{
			ID:           "test-id",
			TokenHash:    "hash",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         nil, // Explicitly nil
		}

		// Should not panic
		sess.SetValue("key", "value")

		val, ok := sess.GetValue("key")
		require.True(t, ok)
		require.Equal(t, "value", val)
	})

	t.Run("GetValue on nil Data returns false", func(t *testing.T) {
		t.Parallel()

		sess := &internal.Session{
			ID:           "test-id",
			TokenHash:    "hash",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         nil,
		}

		val, ok := sess.GetValue("key")
		require.False(t, ok)
		require.Nil(t, val)
	})

	t.Run("DeleteValue on nil Data is no-op", func(t *testing.T) {
		t.Parallel()

		sess := &internal.Session{
			ID:           "test-id",
			TokenHash:    "hash",
			CreatedAt:    time.Now(),
			LastActiveAt: time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
			Data:         nil,
		}

		// Should not panic
		sess.DeleteValue("key")
		require.False(t, sess.IsDirty())
	})
}

// TestStoreInterface_Contracts verifies that Store interface requirements
// are documented and testable by implementers.
func TestStoreInterface_Contracts(t *testing.T) {
	t.Parallel()

	t.Run("store must handle Data serialization", func(t *testing.T) {
		t.Parallel()

		// This test documents that Store implementations must serialize
		// Session.Data map[string]any to/from storage (e.g., JSONB).
		sess := internal.NewSession("token", time.Now().Add(time.Hour))
		sess.SetValue("nested", map[string]any{
			"level1": map[string]any{
				"level2": "deep value",
			},
		})

		// Store implementations must handle this nested structure
		require.NotNil(t, sess.Data)
		require.Contains(t, sess.Data, "nested")
	})

	t.Run("GetByTokenHash must check expiration", func(t *testing.T) {
		t.Parallel()

		// Document that GetByTokenHash should return ErrSessionExpired
		// for expired sessions, not just filter them out.
		expiredSession := internal.NewSession("token", time.Now().Add(-time.Hour))
		require.True(t, expiredSession.IsExpired())

		// Store.GetByTokenHash should detect this and return ErrSessionExpired
	})
}

// TestSession_Concurrency documents that Session is NOT goroutine-safe.
func TestSession_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("concurrent access is not safe", func(t *testing.T) {
		t.Parallel()

		// This test documents that Session methods are NOT goroutine-safe.
		// Users must not share Session instances across goroutines without
		// external synchronization.

		sess := internal.NewSession("token", time.Now().Add(time.Hour))

		// The following would cause data races if uncommented:
		// var wg sync.WaitGroup
		// for i := range 100 {
		// 	wg.Add(1)
		// 	go func(n int) {
		// 		defer wg.Done()
		// 		sess.SetValue("key", n)
		// 		sess.GetValue("key")
		// 	}(i)
		// }
		// wg.Wait()

		// Instead, users should:
		// 1. Only access Session from request context
		// 2. Make copies of values if needed across goroutines
		// 3. Never share Session instance across concurrent requests

		require.NotNil(t, sess)
	})
}

func TestSession_ContextIntegration(t *testing.T) {
	t.Parallel()

	t.Run("session works within request context", func(t *testing.T) {
		t.Parallel()

		// Sessions are designed to be used via forge.Context methods,
		// not constructed directly by users.

		ctx := context.Background()
		sess := internal.NewSession("token", time.Now().Add(time.Hour))

		// Store session-specific request data
		sess.SetValue("cart_items", []string{"item1", "item2"})
		sess.SetValue("csrf_token", "abc123")

		// Verify data survives context
		_ = ctx // Context doesn't affect session data

		items, ok := sess.GetValue("cart_items")
		require.True(t, ok)
		require.Equal(t, []string{"item1", "item2"}, items)

		csrf, ok := sess.GetValue("csrf_token")
		require.True(t, ok)
		require.Equal(t, "abc123", csrf)
	})
}
