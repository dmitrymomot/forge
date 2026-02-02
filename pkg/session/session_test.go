package session

import (
	"testing"
	"time"
)

func TestSession_New(t *testing.T) {
	expiresAt := time.Now().Add(24 * time.Hour)
	sess := New("test-id", "test-token", expiresAt)

	if sess.ID != "test-id" {
		t.Errorf("ID = %q, want %q", sess.ID, "test-id")
	}
	if sess.Token != "test-token" {
		t.Errorf("Token = %q, want %q", sess.Token, "test-token")
	}
	if !sess.IsNew() {
		t.Error("IsNew() = false, want true")
	}
	if !sess.IsDirty() {
		t.Error("IsDirty() = false, want true")
	}
	if sess.Values == nil {
		t.Error("Values is nil")
	}
}

func TestSession_IsAuthenticated(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))

	if sess.IsAuthenticated() {
		t.Error("IsAuthenticated() = true for new session, want false")
	}

	userID := "user-123"
	sess.UserID = &userID

	if !sess.IsAuthenticated() {
		t.Error("IsAuthenticated() = false after setting UserID, want true")
	}

	empty := ""
	sess.UserID = &empty

	if sess.IsAuthenticated() {
		t.Error("IsAuthenticated() = true for empty UserID, want false")
	}
}

func TestSession_Values(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))
	sess.ClearDirty() // Reset dirty state

	sess.SetValue("key", "value")

	if !sess.IsDirty() {
		t.Error("SetValue should mark session as dirty")
	}

	val, ok := sess.GetValue("key")
	if !ok {
		t.Error("GetValue returned ok=false for existing key")
	}
	if val != "value" {
		t.Errorf("GetValue = %v, want %v", val, "value")
	}

	_, ok = sess.GetValue("nonexistent")
	if ok {
		t.Error("GetValue returned ok=true for nonexistent key")
	}
}

func TestSession_DeleteValue(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))
	sess.SetValue("key", "value")
	sess.ClearDirty()

	sess.DeleteValue("key")

	if !sess.IsDirty() {
		t.Error("DeleteValue should mark session as dirty")
	}

	_, ok := sess.GetValue("key")
	if ok {
		t.Error("GetValue returned ok=true after DeleteValue")
	}
}

func TestSession_DirtyFlag(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))

	if !sess.IsDirty() {
		t.Error("new session should be dirty")
	}

	sess.ClearDirty()
	if sess.IsDirty() {
		t.Error("ClearDirty() should clear dirty flag")
	}

	sess.MarkDirty()
	if !sess.IsDirty() {
		t.Error("MarkDirty() should set dirty flag")
	}
}

func TestSession_NewFlag(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))

	if !sess.IsNew() {
		t.Error("new session should have IsNew() = true")
	}

	sess.ClearNew()
	if sess.IsNew() {
		t.Error("ClearNew() should clear new flag")
	}
}

func TestSession_IsExpired(t *testing.T) {
	// Not expired
	sess := New("id", "token", time.Now().Add(time.Hour))
	if sess.IsExpired() {
		t.Error("future expiry should not be expired")
	}

	// Expired
	sess.ExpiresAt = time.Now().Add(-time.Hour)
	if !sess.IsExpired() {
		t.Error("past expiry should be expired")
	}
}

func TestValue_TypedHelper(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))
	sess.SetValue("string", "hello")
	sess.SetValue("int", 42)
	sess.SetValue("bool", true)

	// Test string
	strVal, err := Value[string](sess, "string")
	if err != nil {
		t.Errorf("Value[string] error: %v", err)
	}
	if strVal != "hello" {
		t.Errorf("Value[string] = %q, want %q", strVal, "hello")
	}

	// Test int
	intVal, err := Value[int](sess, "int")
	if err != nil {
		t.Errorf("Value[int] error: %v", err)
	}
	if intVal != 42 {
		t.Errorf("Value[int] = %d, want %d", intVal, 42)
	}

	// Test bool
	boolVal, err := Value[bool](sess, "bool")
	if err != nil {
		t.Errorf("Value[bool] error: %v", err)
	}
	if !boolVal {
		t.Error("Value[bool] = false, want true")
	}

	// Test type mismatch
	_, err = Value[int](sess, "string")
	if err == nil {
		t.Error("Value[int] on string should return error")
	}

	// Test nonexistent key
	_, err = Value[string](sess, "nonexistent")
	if err == nil {
		t.Error("Value on nonexistent key should return error")
	}

	// Test nil session
	_, err = Value[string](nil, "key")
	if err != ErrNotFound {
		t.Errorf("Value on nil session should return ErrNotFound, got %v", err)
	}
}

func TestValueOr_TypedHelper(t *testing.T) {
	sess := New("id", "token", time.Now().Add(time.Hour))
	sess.SetValue("exists", "value")

	// Existing key
	val := ValueOr(sess, "exists", "default")
	if val != "value" {
		t.Errorf("ValueOr = %q, want %q", val, "value")
	}

	// Nonexistent key
	val = ValueOr(sess, "nonexistent", "default")
	if val != "default" {
		t.Errorf("ValueOr for nonexistent = %q, want %q", val, "default")
	}

	// Type mismatch
	intVal := ValueOr(sess, "exists", 42)
	if intVal != 42 {
		t.Errorf("ValueOr for type mismatch = %d, want %d", intVal, 42)
	}
}
