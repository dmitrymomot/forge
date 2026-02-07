// Package jwt provides RFC 7519 compliant JSON Web Token implementation using HMAC-SHA256.
//
// This package includes generation, validation, and parsing of JWTs with support for standard
// claims and custom payload structures. All operations use constant-time comparisons to prevent
// timing attacks and implement security best practices.
//
// # Features
//
// - RFC 7519 compliant JWT implementation
// - HMAC-SHA256 signing (secure and performant)
// - Standard claims validation (exp, nbf, iat)
// - Custom claims support with any JSON-serializable type
// - Constant-time signature verification
// - Built-in temporal claim validation
//
// # Usage
//
// Basic JWT service setup:
//
//	// Create service with signing key (should be cryptographically secure)
//	service, err := jwt.New([]byte("your-256-bit-secret"))
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Or from string
//	service, err := jwt.NewFromString("your-secret-key")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Generating tokens with standard claims:
//
//	claims := jwt.StandardClaims{
//		Subject:   "user123",
//		Issuer:    "myapp",
//		Audience:  "api",
//		ExpiresAt: time.Now().Add(time.Hour).Unix(),
//		IssuedAt:  time.Now().Unix(),
//	}
//
//	token, err := service.Generate(claims)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Generating tokens with custom claims:
//
//	type CustomClaims struct {
//		jwt.StandardClaims
//		UserID   int    `json:"user_id"`
//		Username string `json:"username"`
//		Role     string `json:"role"`
//	}
//
//	claims := CustomClaims{
//		StandardClaims: jwt.StandardClaims{
//			Subject:   "user123",
//			ExpiresAt: time.Now().Add(time.Hour).Unix(),
//			IssuedAt:  time.Now().Unix(),
//		},
//		UserID:   123,
//		Username: "john.doe",
//		Role:     "admin",
//	}
//
//	token, err := service.Generate(claims)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Parsing and validating tokens:
//
//	var claims CustomClaims
//	err := service.Parse(token, &claims)
//	if err != nil {
//		switch {
//		case errors.Is(err, jwt.ErrExpiredToken):
//			log.Println("Token has expired")
//		case errors.Is(err, jwt.ErrInvalidSignature):
//			log.Println("Token signature is invalid")
//		default:
//			log.Printf("Token parsing failed: %v", err)
//		}
//		return
//	}
//
//	log.Printf("Valid token for user: %s (ID: %d)", claims.Username, claims.UserID)
//
// # HTTP Middleware Example
//
//	func JWTMiddleware(service *jwt.Service) func(http.Handler) http.Handler {
//		return func(next http.Handler) http.Handler {
//			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//				authHeader := r.Header.Get("Authorization")
//				if authHeader == "" {
//					http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
//					return
//				}
//
//				// Extract Bearer token
//				token := strings.TrimPrefix(authHeader, "Bearer ")
//				if token == authHeader {
//					http.Error(w, "Invalid Authorization format", http.StatusUnauthorized)
//					return
//				}
//
//				var claims CustomClaims
//				if err := service.Parse(token, &claims); err != nil {
//					http.Error(w, "Invalid token", http.StatusUnauthorized)
//					return
//				}
//
//				// Add claims to request context
//				ctx := context.WithValue(r.Context(), "claims", claims)
//				next.ServeHTTP(w, r.WithContext(ctx))
//			})
//		}
//	}
//
// # Security Best Practices
//
// Signing key requirements:
//   - Use at least 32 bytes (256 bits) for HMAC-SHA256
//   - Generate using cryptographically secure random source
//   - Store securely (environment variables, key management service)
//   - Rotate keys regularly
//
// Token expiration:
//   - Always set ExpiresAt for security tokens
//   - Use short expiration times (15-60 minutes) for access tokens
//   - Implement refresh token mechanism for long-lived sessions
//   - Consider NotBefore (nbf) for tokens issued for future use
//
// Validation considerations:
//   - Always validate signature before trusting claims
//   - Check expiration in addition to signature
//   - Validate audience (aud) claim when using multiple services
//   - Implement token blacklisting for logout/revocation
//
// # Standard Claims
//
// The package supports all RFC 7519 registered claims:
//   - jti (JWT ID): Unique identifier, useful for token blacklisting
//   - sub (Subject): Usually user ID or entity identifier
//   - iss (Issuer): Identifies who issued the token
//   - aud (Audience): Intended recipients of the token
//   - exp (Expiration): Unix timestamp when token expires
//   - nbf (Not Before): Unix timestamp when token becomes valid
//   - iat (Issued At): Unix timestamp when token was created
//
// # Error Handling
//
// The package provides specific error types for different failure modes:
//   - ErrInvalidToken: Malformed token structure or nbf validation failure
//   - ErrExpiredToken: Token past expiration time
//   - ErrInvalidSignature: Signature verification failed
//   - ErrUnexpectedSigningMethod: Algorithm mismatch (security)
//   - ErrInvalidSigningMethod: Invalid signing method (legacy)
//   - ErrMissingSigningKey: Service created without key
//   - ErrInvalidSigningKey: Invalid signing key (available)
//   - ErrInvalidClaims: Invalid claims (available)
//   - ErrMissingClaims: Generate called with nil claims
//
// # Performance Characteristics
//
// - Token generation: ~50-100 µs (dominated by JSON marshaling)
// - Token parsing: ~100-200 µs (includes signature verification)
// - Memory usage: ~1-2 KB per token (depends on claims size)
// - HMAC-SHA256 is significantly faster than RSA signatures
// - Base64 encoding/decoding is optimized and minimal overhead
//
// # Algorithm Choice
//
// HMAC-SHA256 is chosen for this implementation because:
//   - Symmetric key operation (simpler key management)
//   - High performance (much faster than RSA)
//   - Strong security properties
//   - Widespread support and standardization
//   - Suitable for most web application use cases
//
// Consider RSA/ECDSA for scenarios requiring:
//   - Public key verification (microservices)
//   - Key distribution to untrusted parties
//   - Integration with existing PKI infrastructure
package jwt
