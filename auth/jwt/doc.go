// Package jwt signs and verifies JSON Web Tokens with a pinned algorithm
// allowlist — HS256, RS256, ES256, EdDSA — that is never negotiated. Every
// key is bound to exactly one algorithm at construction and the token's
// alg header must match the resolved key's algorithm, which structurally
// rules out alg:none, algorithm-swap, and HMAC/public-key confusion
// attacks. There is no JWE and no algorithm registry.
//
// Signing and verifying are split: a Signer issues tokens with its primary
// key and publishes its asymmetric public keys over an RFC 7517 JWKS
// handler; a Verifier checks tokens from static keys, a keyset, or a
// remote JWKS URL with in-memory caching, TTL refresh, and unknown-kid
// refetch behind a cooldown. Key rotation rides crypto/keyset: the primary
// version signs, retired versions keep verifying, and the keyset version
// is the kid.
//
// Claims are typed: embed Claims in a consumer struct and Verify returns
// it decoded, with exp/nbf/iss/aud already enforced (30s default leeway;
// missing exp is rejected unless WithoutExpiry is set). Sign marshals
// exactly the claims it is given — nothing is auto-filled.
//
// # Usage
//
// Simple shared-secret API (HS256 over one env-loaded keyset):
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("JWT_KEYS")))
//	signer, _ := jwt.NewSigner(jwt.WithHS256Keyset(ks))
//	verifier, _ := jwt.NewVerifier(
//		jwt.WithVerifyHS256Keyset(ks),
//		jwt.WithIssuer("https://api.example.com"),
//	)
//
//	type AccessClaims struct {
//		jwt.Claims
//		TenantID string `json:"tid"`
//	}
//
//	token, _ := signer.Sign(AccessClaims{
//		Claims: jwt.Claims{
//			Issuer:    "https://api.example.com",
//			Subject:   userID,
//			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
//		},
//		TenantID: tenantID,
//	})
//
//	claims, err := jwt.Verify[AccessClaims](ctx, verifier, token)
//
// Asymmetric issuing with a JWKS endpoint for external verifiers:
//
//	signer, _ := jwt.NewSigner(jwt.WithKeyset(ks)) // PKCS#8 DER material
//	mux.Handle("/.well-known/jwks.json", signer.JWKS(jwt.WithCacheControl(5*time.Minute)))
//
// Verifying a third party's tokens from their JWKS:
//
//	verifier, _ := jwt.NewVerifier(
//		jwt.WithJWKSURL("https://issuer.example.com/.well-known/jwks.json"),
//		jwt.WithIssuer("https://issuer.example.com"),
//		jwt.WithAudience("my-client-id"),
//	)
package jwt
