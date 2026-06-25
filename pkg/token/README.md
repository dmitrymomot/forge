# token

Compact, URL-safe signed tokens backed by a truncated HMAC-SHA256.

Tokens encode any JSON-serializable payload into a short string suitable for email
verification links, invite tokens, magic links, and password-reset URLs. Unlike JWTs,
these tokens carry no header and no registered claims — just a signed payload. The
package is stdlib-only with no external dependencies.

## Token format

A token is two base64url-encoded segments separated by a dot:

```
base64url(json(payload)).base64url(hmac-sha256[:8])
```

The signature is a truncated HMAC-SHA256 — the first 8 bytes (64 bits) of the digest.
The entire token is URL-safe: no padding and no `+`, `/`, or `=` characters.

## Usage

### Generate a token

```go
type EmailVerification struct {
    UserID string `json:"user_id"`
    Email  string `json:"email"`
    Action string `json:"action"`
}

payload := EmailVerification{
    UserID: "usr_abc123",
    Email:  "user@example.com",
    Action: "verify_email",
}

tok, err := token.GenerateToken(payload, []byte("your-32-byte-secret-key-here..."))
if err != nil {
    return err
}

verifyURL := fmt.Sprintf("https://example.com/verify?token=%s", tok)
```

`GenerateToken` returns `ErrEmptySecret` if the secret is nil or empty, and wraps any
JSON marshal failure (e.g. a payload containing a channel or function value).

### Parse and verify a token

```go
result, err := token.ParseToken[EmailVerification](tok, secret)
if err != nil {
    switch {
    case errors.Is(err, token.ErrSignatureInvalid):
        // tampered payload/signature or wrong secret
    case errors.Is(err, token.ErrInvalidToken):
        // malformed structure or invalid base64url encoding
    case errors.Is(err, token.ErrEmptySecret):
        // nil or empty secret
    }
    return
}

log.Printf("verified email %s for user %s", result.Email, result.UserID)
```

`ParseToken` verifies the signature in constant time **before** decoding or
unmarshaling the payload, so a tampered payload is never processed.

## Errors

| Error                 | Meaning                                                              |
| --------------------- | ------------------------------------------------------------------- |
| `ErrInvalidToken`     | Missing separator, empty segment, or invalid base64url encoding.    |
| `ErrSignatureInvalid` | HMAC signature does not match (tampered token or wrong secret).     |
| `ErrEmptySecret`      | A nil or empty secret was provided.                                 |

## Security notes

### Secret key

- Use at least 32 bytes (256 bits) for HMAC-SHA256.
- Generate it from a cryptographically secure random source.
- Store it securely (environment variable, key-management service).

### 64-bit truncation tradeoff

The signature is truncated to 8 bytes (64 bits), giving roughly 2^64 brute-force
resistance. This keeps tokens compact and is adequate for **short-lived, single-use**
tokens that are also gated by server-side state. It is intentionally **not** suitable
for long-lived bearer tokens — use the `jwt` package for those.

### Replay protection

Signature verification proves authenticity, not freshness. This package has no
built-in expiration or single-use enforcement, so you must add it yourself:

- Pair every token with server-side state (a database flag or one-time nonce).
- Mark a token as consumed after first use to prevent replay.
- Enforce expiration at the application level (e.g. store an issued-at/expires-at
  value alongside the token's server-side record).

## When to use token vs jwt

Use `token` for short-lived, single-use values embedded in URLs or emails where
compact size matters and there are no temporal claims. Use `jwt` for API
authentication tokens that need standard claims, longer signatures, and header
metadata.
