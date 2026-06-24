# fingerprint

Device fingerprinting from HTTP requests for session validation and lightweight session-hijacking detection.

The package derives a stable, version-prefixed fingerprint from request characteristics (User-Agent, Accept headers, the set of present headers, and optionally the client IP). Store the fingerprint with the session at login and validate it on subsequent requests; a mismatch signals that client characteristics changed.

This is a defense-in-depth signal, not a replacement for proper session management.

## Install

```go
import "github.com/dmitrymomot/forge/pkg/fingerprint"
```

## Usage

```go
// At login: generate and store with the session.
fp := fingerprint.Generate(r, fingerprint.DefaultConfig())
session.Set("fingerprint", fp) // e.g. "v1:a1b2c3d4..." (35 chars)

// On later requests: validate. Use the SAME Config used to generate.
err := fingerprint.Validate(r, storedFP, fingerprint.DefaultConfig())
switch {
case err == nil:
    // matches
case errors.Is(err, fingerprint.ErrMismatch):
    // client characteristics changed (possible hijacking or a benign change)
case errors.Is(err, fingerprint.ErrInvalidFingerprint):
    // stored value is not a valid "v1:hash" string
}
```

Comparison in `Validate` is constant-time.

## Generators and validators

Each generator has a matching validator that uses the same configuration. Use the matching pair.

| Generator | Validator | Components | Use case |
|-----------|-----------|------------|----------|
| `Cookie(r)` | `ValidateCookie(r, fp)` | User-Agent, Accept headers, header set (no IP) | Recommended default for cookie sessions |
| `JWT(r)` | `ValidateJWT(r, fp)` | User-Agent, header set (no Accept headers) | JWT auth where Accept varies with content negotiation |
| `HTMX(r)` | `ValidateHTMX(r, fp)` | User-Agent only | HTMX apps, avoids false positives from HX-* and varying Accept headers |
| `Strict(r)` | `ValidateStrict(r, fp)` | All of the above plus client IP | High-security only; causes false positives for mobile/VPN users |

For full control, use `Generate(r, cfg)` / `Validate(r, fp, cfg)` with a custom `Config` (`IncludeIP`, `IncludeUserAgent`, `IncludeAcceptHeaders`, `IncludeHeaderSet`). When validating, always pass the same `Config` used to generate.

## Format

```
v1:<32 hex chars>
```

- `v1` — algorithm version; the prefix lets the algorithm change without breaking stored fingerprints.
- hash — hex encoding of the first 16 bytes (128 bits) of a SHA-256 over the selected components, joined with a `|` delimiter to avoid component-concatenation collisions.
- Total length is always 35 characters.

## Errors

- `ErrInvalidFingerprint` — the stored fingerprint is not a well-formed `v1:hash` string.
- `ErrMismatch` — the fingerprint does not match the current request.

Both are checkable with `errors.Is`.

## Notes

- Functions are stateless and safe for concurrent use.
- Client IP is resolved via `github.com/dmitrymomot/forge/pkg/clientip`.
- Fingerprints are a weak signal: IP changes, browser updates, and privacy extensions all shift them. Log mismatches and prefer re-authentication over immediate session termination.
