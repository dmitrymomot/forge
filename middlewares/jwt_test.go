package middlewares_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/middlewares"
	"github.com/dmitrymomot/forge/pkg/jwt"
)

const testJWTSecret = "test-secret-key-at-least-32-bytes!"

func newJWTService(t *testing.T) *jwt.Service {
	t.Helper()
	svc, err := jwt.NewFromString(testJWTSecret)
	require.NoError(t, err)
	return svc
}

func generateToken(t *testing.T, svc *jwt.Service, claims any) string {
	t.Helper()
	token, err := svc.Generate(claims)
	require.NoError(t, err)
	return token
}

type customClaims struct {
	jwt.StandardClaims
	UserID int    `json:"user_id"`
	Role   string `json:"role"`
}

func (c customClaims) Valid() error {
	return c.StandardClaims.Valid()
}

func TestJWTMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("valid token with StandardClaims", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		claims := jwt.StandardClaims{
			Subject:   "user-123",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
		}
		token := generateToken(t, svc, claims)

		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotClaims *jwt.StandardClaims
		handler := mw(func(c internal.Context) error {
			gotClaims = middlewares.GetJWTClaims[jwt.StandardClaims](c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.NotNil(t, gotClaims)
		require.Equal(t, "user-123", gotClaims.Subject)
	})

	t.Run("valid token with custom claims", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		claims := customClaims{
			StandardClaims: jwt.StandardClaims{
				Subject:   "user-456",
				ExpiresAt: time.Now().Add(time.Hour).Unix(),
				IssuedAt:  time.Now().Unix(),
			},
			UserID: 456,
			Role:   "admin",
		}
		token := generateToken(t, svc, claims)

		mw := middlewares.JWT[customClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotClaims *customClaims
		handler := mw(func(c internal.Context) error {
			gotClaims = middlewares.GetJWTClaims[customClaims](c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.NotNil(t, gotClaims)
		require.Equal(t, "user-456", gotClaims.Subject)
		require.Equal(t, 456, gotClaims.UserID)
		require.Equal(t, "admin", gotClaims.Role)
	})

	t.Run("missing authorization header", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(c)
		require.Error(t, err)
		var httpErr *internal.HTTPError
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
		require.Equal(t, "missing authentication token", httpErr.Message)
	})

	t.Run("malformed token", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer not-a-valid-jwt")
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(c)
		require.Error(t, err)
		var httpErr *internal.HTTPError
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
		require.Equal(t, "invalid token", httpErr.Message)
	})

	t.Run("expired token", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		claims := jwt.StandardClaims{
			Subject:   "user-789",
			ExpiresAt: time.Now().Add(-time.Hour).Unix(),
			IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		}
		token := generateToken(t, svc, claims)

		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		handler := mw(func(c internal.Context) error {
			return nil
		})

		err := handler(c)
		require.Error(t, err)
		var httpErr *internal.HTTPError
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
		require.Equal(t, "token expired", httpErr.Message)
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		// Generate token with a different key
		otherSvc, err := jwt.NewFromString("a-completely-different-secret-key!!")
		require.NoError(t, err)
		claims := jwt.StandardClaims{
			Subject:   "user-000",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
		}
		token := generateToken(t, otherSvc, claims)

		// Validate with the original service
		svc := newJWTService(t)
		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		handler := mw(func(c internal.Context) error {
			return nil
		})

		err = handler(c)
		require.Error(t, err)
		var httpErr *internal.HTTPError
		require.True(t, errors.As(err, &httpErr))
		require.Equal(t, http.StatusUnauthorized, httpErr.Code)
		require.Equal(t, "invalid token", httpErr.Message)
	})

	t.Run("custom extractor from query", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		claims := jwt.StandardClaims{
			Subject:   "user-query",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
		}
		token := generateToken(t, svc, claims)

		ext := internal.NewExtractor(internal.FromQuery("token"))
		mw := middlewares.JWT[jwt.StandardClaims](svc, middlewares.WithJWTExtractor(ext))

		r := httptest.NewRequest(http.MethodGet, "/?token="+token, nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotClaims *jwt.StandardClaims
		handler := mw(func(c internal.Context) error {
			gotClaims = middlewares.GetJWTClaims[jwt.StandardClaims](c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.NotNil(t, gotClaims)
		require.Equal(t, "user-query", gotClaims.Subject)
	})
}

func TestGetJWTClaims(t *testing.T) {
	t.Parallel()

	t.Run("wrong type returns nil", func(t *testing.T) {
		t.Parallel()
		svc := newJWTService(t)
		claims := jwt.StandardClaims{
			Subject:   "user-typed",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
		}
		token := generateToken(t, svc, claims)

		// Middleware parses as StandardClaims
		mw := middlewares.JWT[jwt.StandardClaims](svc)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		var gotCustom *customClaims
		handler := mw(func(c internal.Context) error {
			// Try to get as customClaims — should return nil
			gotCustom = middlewares.GetJWTClaims[customClaims](c)
			return nil
		})

		err := handler(c)
		require.NoError(t, err)
		require.Nil(t, gotCustom)
	})

	t.Run("without middleware returns nil", func(t *testing.T) {
		t.Parallel()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		c := newTestContext(w, r)

		got := middlewares.GetJWTClaims[jwt.StandardClaims](c)
		require.Nil(t, got)
	})
}
