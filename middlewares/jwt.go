package middlewares

import (
	"errors"

	"github.com/dmitrymomot/forge/internal"
	"github.com/dmitrymomot/forge/pkg/jwt"
)

// JWTConfig configures the JWT middleware.
type JWTConfig struct {
	Extractor    internal.Extractor
	extractorSet bool
}

// JWTOption configures JWTConfig.
type JWTOption func(*JWTConfig)

// WithJWTExtractor sets a custom token extractor chain.
func WithJWTExtractor(ext internal.Extractor) JWTOption {
	return func(cfg *JWTConfig) {
		cfg.Extractor = ext
		cfg.extractorSet = true
	}
}

// JWT returns middleware that extracts a JWT from the request, validates it,
// and stores the parsed claims in the context.
// T is the claims type to parse into (e.g., jwt.StandardClaims or a custom struct).
func JWT[T any](svc *jwt.Service, opts ...JWTOption) internal.Middleware {
	cfg := &JWTConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Default extractor: Bearer token from Authorization header
	if !cfg.extractorSet {
		cfg.Extractor = internal.NewExtractor(
			internal.FromBearerToken(),
		)
	}

	return func(next internal.HandlerFunc) internal.HandlerFunc {
		return func(c internal.Context) error {
			token, ok := cfg.Extractor.Extract(c)
			if !ok || token == "" {
				return internal.ErrUnauthorized("missing authentication token")
			}

			var claims T
			if err := svc.Parse(token, &claims); err != nil {
				switch {
				case errors.Is(err, jwt.ErrExpiredToken):
					return internal.ErrUnauthorized("token expired")
				case errors.Is(err, jwt.ErrInvalidSignature):
					return internal.ErrUnauthorized("invalid token")
				default:
					return internal.ErrUnauthorized("invalid token")
				}
			}

			c.Set(internal.JWTClaimsKey{}, &claims)

			return next(c)
		}
	}
}

// GetJWTClaims extracts parsed JWT claims from the context.
// Returns nil if the JWT middleware is not applied or the type doesn't match.
func GetJWTClaims[T any](c internal.Context) *T {
	v, ok := c.Get(internal.JWTClaimsKey{}).(*T)
	if !ok {
		return nil
	}
	return v
}
