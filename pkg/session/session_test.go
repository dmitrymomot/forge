package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSession_New(t *testing.T) {
	t.Parallel()

	t.Run("creates session with all fields initialized", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().Add(24 * time.Hour)
		sess := New("test-id", "test-token", expiresAt)

		assert.Equal(t, "test-id", sess.ID)
		assert.Equal(t, "test-token", sess.Token)
		assert.True(t, sess.IsNew())
		assert.True(t, sess.IsDirty())
		assert.NotNil(t, sess.Values)
		assert.Empty(t, sess.Values)
		assert.False(t, sess.CreatedAt.IsZero())
		assert.False(t, sess.LastActiveAt.IsZero())
		assert.Equal(t, expiresAt, sess.ExpiresAt)
	})

	t.Run("creates session with empty strings", func(t *testing.T) {
		t.Parallel()

		sess := New("", "", time.Time{})

		assert.Equal(t, "", sess.ID)
		assert.Equal(t, "", sess.Token)
		assert.True(t, sess.IsNew())
		assert.NotNil(t, sess.Values)
	})
}

func TestSession_IsAuthenticated(t *testing.T) {
	t.Parallel()

	t.Run("returns false for new session", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		assert.False(t, sess.IsAuthenticated())
	})

	t.Run("returns true when UserID is set", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		userID := "user-123"
		sess.UserID = &userID

		assert.True(t, sess.IsAuthenticated())
	})

	t.Run("returns false for empty UserID string", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		empty := ""
		sess.UserID = &empty

		assert.False(t, sess.IsAuthenticated())
	})

	t.Run("returns false when UserID is nil", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.UserID = nil

		assert.False(t, sess.IsAuthenticated())
	})
}

func TestSession_SetValue(t *testing.T) {
	t.Parallel()

	t.Run("sets value and marks as dirty", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		sess.SetValue("key", "value")

		assert.True(t, sess.IsDirty())
		val, ok := sess.GetValue("key")
		require.True(t, ok)
		assert.Equal(t, "value", val)
	})

	t.Run("initializes Values map if nil", func(t *testing.T) {
		t.Parallel()

		sess := &Session{Values: nil}
		sess.SetValue("key", "value")

		require.NotNil(t, sess.Values)
		val, ok := sess.GetValue("key")
		require.True(t, ok)
		assert.Equal(t, "value", val)
	})

	t.Run("overwrites existing value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("key", "first")
		sess.SetValue("key", "second")

		val, ok := sess.GetValue("key")
		require.True(t, ok)
		assert.Equal(t, "second", val)
	})

	t.Run("stores nil value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("key", nil)

		val, ok := sess.GetValue("key")
		require.True(t, ok)
		assert.Nil(t, val)
	})

	t.Run("stores complex types", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))

		type testStruct struct {
			Name  string
			Count int
		}

		expected := testStruct{Name: "test", Count: 42}
		sess.SetValue("struct", expected)

		val, ok := sess.GetValue("struct")
		require.True(t, ok)
		assert.Equal(t, expected, val)
	})
}

func TestSession_GetValue(t *testing.T) {
	t.Parallel()

	t.Run("returns value and true for existing key", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("key", "value")

		val, ok := sess.GetValue("key")
		require.True(t, ok)
		assert.Equal(t, "value", val)
	})

	t.Run("returns nil and false for nonexistent key", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))

		val, ok := sess.GetValue("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, val)
	})

	t.Run("returns nil and false when Values is nil", func(t *testing.T) {
		t.Parallel()

		sess := &Session{Values: nil}

		val, ok := sess.GetValue("key")
		assert.False(t, ok)
		assert.Nil(t, val)
	})
}

func TestSession_DeleteValue(t *testing.T) {
	t.Parallel()

	t.Run("deletes value and marks as dirty", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("key", "value")
		sess.ClearDirty()

		sess.DeleteValue("key")

		assert.True(t, sess.IsDirty())
		_, ok := sess.GetValue("key")
		assert.False(t, ok)
	})

	t.Run("does nothing when Values is nil", func(t *testing.T) {
		t.Parallel()

		sess := &Session{Values: nil}
		sess.DeleteValue("key")

		assert.False(t, sess.IsDirty())
	})

	t.Run("does not mark dirty for nonexistent key", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		sess.DeleteValue("nonexistent")

		assert.False(t, sess.IsDirty())
	})
}

func TestSession_DirtyFlag(t *testing.T) {
	t.Parallel()

	t.Run("new session is dirty", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		assert.True(t, sess.IsDirty())
	})

	t.Run("ClearDirty clears flag", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.ClearDirty()

		assert.False(t, sess.IsDirty())
	})

	t.Run("MarkDirty sets flag", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.ClearDirty()
		sess.MarkDirty()

		assert.True(t, sess.IsDirty())
	})
}

func TestSession_NewFlag(t *testing.T) {
	t.Parallel()

	t.Run("new session has IsNew true", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		assert.True(t, sess.IsNew())
	})

	t.Run("ClearNew clears flag", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.ClearNew()

		assert.False(t, sess.IsNew())
	})
}

func TestSession_IsExpired(t *testing.T) {
	t.Parallel()

	t.Run("returns false for future expiry", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		assert.False(t, sess.IsExpired())
	})

	t.Run("returns true for past expiry", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(-time.Hour))
		assert.True(t, sess.IsExpired())
	})

	t.Run("returns true for expiry exactly now", func(t *testing.T) {
		t.Parallel()

		// Set expiry slightly in the past to ensure it's expired
		sess := New("id", "token", time.Now().Add(-time.Millisecond))
		time.Sleep(2 * time.Millisecond)

		assert.True(t, sess.IsExpired())
	})

	t.Run("returns false for very short future expiry", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Millisecond))
		assert.False(t, sess.IsExpired())
	})
}

func TestValue_TypedHelper(t *testing.T) {
	t.Parallel()

	t.Run("retrieves string value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("string", "hello")

		val, err := Value[string](sess, "string")
		require.NoError(t, err)
		assert.Equal(t, "hello", val)
	})

	t.Run("retrieves int value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("int", 42)

		val, err := Value[int](sess, "int")
		require.NoError(t, err)
		assert.Equal(t, 42, val)
	})

	t.Run("retrieves bool value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("bool", true)

		val, err := Value[bool](sess, "bool")
		require.NoError(t, err)
		assert.True(t, val)
	})

	t.Run("returns error for type mismatch", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("string", "hello")

		_, err := Value[int](sess, "string")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
	})

	t.Run("returns ErrNotFound for nonexistent key", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))

		_, err := Value[string](sess, "nonexistent")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("returns ErrNotFound for nil session", func(t *testing.T) {
		t.Parallel()

		_, err := Value[string](nil, "key")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("retrieves complex struct type", func(t *testing.T) {
		t.Parallel()

		type User struct {
			ID    string
			Email string
		}

		sess := New("id", "token", time.Now().Add(time.Hour))
		expected := User{ID: "123", Email: "test@example.com"}
		sess.SetValue("user", expected)

		val, err := Value[User](sess, "user")
		require.NoError(t, err)
		assert.Equal(t, expected, val)
	})

	t.Run("retrieves slice type", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		expected := []string{"a", "b", "c"}
		sess.SetValue("slice", expected)

		val, err := Value[[]string](sess, "slice")
		require.NoError(t, err)
		assert.Equal(t, expected, val)
	})

	t.Run("retrieves map type", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		expected := map[string]int{"a": 1, "b": 2}
		sess.SetValue("map", expected)

		val, err := Value[map[string]int](sess, "map")
		require.NoError(t, err)
		assert.Equal(t, expected, val)
	})

	t.Run("handles nil stored value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("nil", nil)

		// This should fail type assertion since nil cannot be asserted to string
		_, err := Value[string](sess, "nil")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "type mismatch")
	})
}

func TestValueOr_TypedHelper(t *testing.T) {
	t.Parallel()

	t.Run("returns existing value", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("exists", "value")

		val := ValueOr(sess, "exists", "default")
		assert.Equal(t, "value", val)
	})

	t.Run("returns default for nonexistent key", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))

		val := ValueOr(sess, "nonexistent", "default")
		assert.Equal(t, "default", val)
	})

	t.Run("returns default for type mismatch", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))
		sess.SetValue("exists", "string value")

		val := ValueOr(sess, "exists", 42)
		assert.Equal(t, 42, val)
	})

	t.Run("returns default for nil session", func(t *testing.T) {
		t.Parallel()

		val := ValueOr[string](nil, "key", "default")
		assert.Equal(t, "default", val)
	})

	t.Run("returns zero value as default", func(t *testing.T) {
		t.Parallel()

		sess := New("id", "token", time.Now().Add(time.Hour))

		val := ValueOr(sess, "nonexistent", 0)
		assert.Equal(t, 0, val)
	})

	t.Run("works with complex types", func(t *testing.T) {
		t.Parallel()

		type Config struct {
			Enabled bool
		}

		sess := New("id", "token", time.Now().Add(time.Hour))
		expected := Config{Enabled: true}
		sess.SetValue("config", expected)

		val := ValueOr(sess, "config", Config{Enabled: false})
		assert.Equal(t, expected, val)
	})
}
