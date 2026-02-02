package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/pkg/session"
)

// mockStore implements session.Store for testing.
type mockStore struct {
	sessions map[string]*session.Session
	onUpdate func(s *session.Session) error
}

func newMockStore() *mockStore {
	return &mockStore{
		sessions: make(map[string]*session.Session),
	}
}

func (s *mockStore) Create(ctx context.Context, sess *session.Session) error {
	s.sessions[sess.Token] = sess
	return nil
}

func (s *mockStore) Get(ctx context.Context, token string) (*session.Session, error) {
	sess, ok := s.sessions[token]
	if !ok {
		return nil, session.ErrNotFound
	}
	if sess.IsExpired() {
		return nil, session.ErrExpired
	}
	return sess, nil
}

func (s *mockStore) Update(ctx context.Context, sess *session.Session) error {
	if s.onUpdate != nil {
		return s.onUpdate(sess)
	}
	// Update by token lookup, since token can change
	for token := range s.sessions {
		if s.sessions[token].ID == sess.ID {
			delete(s.sessions, token)
			break
		}
	}
	s.sessions[sess.Token] = sess
	return nil
}

func (s *mockStore) Delete(ctx context.Context, id string) error {
	for token, sess := range s.sessions {
		if sess.ID == id {
			delete(s.sessions, token)
			return nil
		}
	}
	return nil
}

func (s *mockStore) DeleteByUserID(ctx context.Context, userID string) error {
	for token, sess := range s.sessions {
		if sess.UserID != nil && *sess.UserID == userID {
			delete(s.sessions, token)
		}
	}
	return nil
}

func (s *mockStore) Touch(ctx context.Context, id string, lastActiveAt time.Time) error {
	for _, sess := range s.sessions {
		if sess.ID == id {
			sess.LastActiveAt = lastActiveAt
			return nil
		}
	}
	return nil
}

func TestSessionManager_CreateSession(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	req.RemoteAddr = "192.168.1.1:12345"

	ctx := context.Background()
	sess, err := sm.CreateSession(ctx, req)

	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}
	if sess == nil {
		t.Fatal("CreateSession() returned nil session")
	}
	if sess.ID == "" {
		t.Error("session ID is empty")
	}
	if sess.Token == "" {
		t.Error("session Token is empty")
	}
	if sess.IP != "192.168.1.1" {
		t.Errorf("IP = %q, want %q", sess.IP, "192.168.1.1")
	}
	if sess.Device != "Chrome on macOS" {
		t.Errorf("Device = %q, want %q", sess.Device, "Chrome on macOS")
	}
	if !sess.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}
}

func TestSessionManager_LoadSession(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store)

	// Create a session first
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.Background()
	created, _ := sm.CreateSession(ctx, req)

	// Create a request with the session cookie
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: "__sid", Value: created.Token})

	loaded, err := sm.LoadSession(ctx, req2)

	if err != nil {
		t.Fatalf("LoadSession() error: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSession() returned nil")
	}
	if loaded.ID != created.ID {
		t.Errorf("loaded ID = %q, want %q", loaded.ID, created.ID)
	}
}

func TestSessionManager_LoadSession_NoCookie(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.Background()

	sess, err := sm.LoadSession(ctx, req)

	if err != nil {
		t.Fatalf("LoadSession() error: %v", err)
	}
	if sess != nil {
		t.Error("LoadSession() should return nil for request without cookie")
	}
}

func TestSessionManager_SaveSession(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store,
		WithSessionCookieName("test-sid"),
		WithSessionSecure(true),
		WithSessionHTTPOnly(true),
	)

	sess := session.New("id", "token123", time.Now().Add(time.Hour))

	w := httptest.NewRecorder()
	sm.SaveSession(w, sess)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "test-sid" {
		t.Errorf("cookie name = %q, want %q", cookie.Name, "test-sid")
	}
	if cookie.Value != "token123" {
		t.Errorf("cookie value = %q, want %q", cookie.Value, "token123")
	}
	if !cookie.Secure {
		t.Error("cookie Secure = false, want true")
	}
	if !cookie.HttpOnly {
		t.Error("cookie HttpOnly = false, want true")
	}
}

func TestSessionManager_RotateToken(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.Background()
	sess, _ := sm.CreateSession(ctx, req)

	oldToken := sess.Token

	err := sm.RotateToken(ctx, sess)

	if err != nil {
		t.Fatalf("RotateToken() error: %v", err)
	}
	if sess.Token == oldToken {
		t.Error("token was not rotated")
	}
	if !sess.IsDirty() {
		t.Error("session should be marked dirty after rotation")
	}
}

func TestSessionManager_DeleteSession(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store, WithSessionCookieName("test-sid"))

	w := httptest.NewRecorder()
	sm.DeleteSession(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.MaxAge != -1 {
		t.Errorf("cookie MaxAge = %d, want -1", cookie.MaxAge)
	}
}

func TestSessionManager_Options(t *testing.T) {
	store := newMockStore()
	sm := NewSessionManager(store,
		WithSessionCookieName("custom"),
		WithSessionMaxAge(3600),
		WithSessionDomain("example.com"),
		WithSessionPath("/app"),
		WithSessionSecure(true),
		WithSessionHTTPOnly(false),
		WithSessionSameSite(http.SameSiteStrictMode),
	)

	if sm.cookieName != "custom" {
		t.Errorf("cookieName = %q, want %q", sm.cookieName, "custom")
	}
	if sm.maxAge != 3600 {
		t.Errorf("maxAge = %d, want %d", sm.maxAge, 3600)
	}
	if sm.domain != "example.com" {
		t.Errorf("domain = %q, want %q", sm.domain, "example.com")
	}
	if sm.path != "/app" {
		t.Errorf("path = %q, want %q", sm.path, "/app")
	}
	if !sm.secure {
		t.Error("secure = false, want true")
	}
	if sm.httpOnly {
		t.Error("httpOnly = true, want false")
	}
	if sm.sameSite != http.SameSiteStrictMode {
		t.Errorf("sameSite = %v, want %v", sm.sameSite, http.SameSiteStrictMode)
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expected      string
	}{
		{
			name:       "remote addr only",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:          "X-Forwarded-For",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.195, 70.41.3.18, 150.172.238.178",
			expected:      "203.0.113.195",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.195",
			expected:   "203.0.113.195",
		},
		{
			name:          "X-Forwarded-For takes precedence",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "1.1.1.1",
			xRealIP:       "2.2.2.2",
			expected:      "1.1.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			ip := extractIP(req)
			if ip != tt.expected {
				t.Errorf("extractIP() = %q, want %q", ip, tt.expected)
			}
		})
	}
}

func TestParseDevice(t *testing.T) {
	tests := []struct {
		userAgent string
		expected  string
	}{
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "Chrome on macOS"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "Chrome on Windows"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/121.0", "Firefox on Linux"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1", "Safari on iOS"},
		{"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.43 Mobile Safari/537.36", "Chrome on Android"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0", "Edge on Windows"},
		{"", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := parseDevice(tt.userAgent)
			if result != tt.expected {
				t.Errorf("parseDevice(%q) = %q, want %q", tt.userAgent, result, tt.expected)
			}
		})
	}
}
