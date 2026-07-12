# web/fingerprint — collector & signal completeness bundle — design

Date: 2026-07-13
Status: approved for planning

## Purpose

Close the coverage gaps in the shipped `web/fingerprint` package (PR #44). The
package's `Collector`/`Component`/`Signal` seams are in place; this bundle fills
in the fingerprinting surfaces they don't yet reach — modern Client Hints, a
self-terminated-TLS collector, a deeper JS device probe, the derived cross-layer
signals those unlock, and making the inert `tls-ua-mismatch` signal usable.

Scope decided during brainstorming: **everything (option D)** across the four
gap groups, packaged as **one PR** because it all lives in one package
(`web/fingerprint` + its `tlsprint` subpackage) and shares one idiom. Output
stays **identity + facts, never a score** — unchanged from the parent design.

Nothing here is a breaking API change: every new collector is opt-in, and the
only behavior shift for existing consumers is that the `Session`/`Antifraud`
presets emit additional components (see §Cross-cutting: hash-change note).

## Non-goals / icebox (documented in `doc.go`)

- **JA4H** (HTTP-layer JA4+). Its discriminating part `JA4H_b` hashes request
  headers in send-order; Go's net/http stores headers in a `map`, so order is
  gone before a handler runs, and HTTP/2 normalizes it at the protocol layer.
  A faithful JA4H would need raw-byte header capture and would only work on the
  minority h1 path. Skipped; documented.
- **HTTP/2 SETTINGS / Akamai h2 fingerprint.** Same root cause — needs
  protocol-frame access net/http doesn't expose. Skipped; documented.
- **Persistent device-cookie ID.** That's `auth/session`'s concern, not this
  package's.
- **Font/audio raw enumeration exposure.** The probe emits *hashes* of these,
  never the raw lists (see Unit 3).

## Units

### Unit 1 — `ClientHints()` collector (new file `clienthints.go`)

Table-driven, exactly like `Headers()` in `headers.go`. Emits **stable
device/browser identity** components only. Absent or blank headers contribute
nothing.

| Request header | Component name |
|---|---|
| `Sec-CH-UA` | `ch-ua` |
| `Sec-CH-UA-Platform` | `ch-ua-platform` |
| `Sec-CH-UA-Mobile` | `ch-ua-mobile` |
| `Sec-CH-UA-Arch` | `ch-ua-arch` |
| `Sec-CH-UA-Bitness` | `ch-ua-bitness` |
| `Sec-CH-UA-Model` | `ch-ua-model` |
| `Device-Memory` | `device-memory` |
| `DPR` | `dpr` |

**Deliberately excluded:**

- `Sec-CH-UA-Platform-Version`, `Sec-CH-UA-Full-Version-List` — churn on every
  browser/OS auto-update; their version entropy is already carried by the `ua`
  component. Dropping them keeps `Drift` quiet.
- `Sec-Fetch-*` — per-request-context, not per-device (a navigation vs. an XHR
  sends different values), so hashing them would flip the same device's
  fingerprint between requests. They are **signal-only inputs** — see Unit 4.

Values are trimmed like `Headers()` does; no parsing (raw stable strings are
what the hash wants). Length is naturally bounded by header size; no extra clamp
needed.

### Unit 2 — `tlsprint.RequestTLS()` collector

For the middle-ground deployment: the Go server terminates `crypto/tls` itself,
with no CDN JA header and no wrapped `tlsprint.Listener`. Reads
`r.TLS *tls.ConnectionState`; when non-nil, emits a single component:

- Name: **`tlsconn`** (distinct from `tls` on purpose)
- Value: `"<version>|<cipher-hex>|<alpn>"`, e.g. `1.3|c02b|h2`
  - version: `"1.2"`/`"1.3"` from `ConnectionState.Version`
  - cipher: 4-hex-digit `ConnectionState.CipherSuite`
  - alpn: `ConnectionState.NegotiatedProtocol` (may be empty)

Rationale for the distinct name: `RequestTLS` produces coarse handshake params,
**not** a JA3/JA4 hash, so it must not land under `tls` where
`tlsUAMismatch` looks values up in `automationJA4` (it never would match, and
mixing a JA hash and a params string under one name would corrupt `Drift` for a
deployment that sometimes has CDN JA4 and sometimes only `r.TLS`). `tlsconn`
is a low-entropy but zero-dependency identity contributor and a gross-anomaly
tell (e.g. a "Chrome" UA over TLS 1.0). `r.TLS == nil` → contributes nothing.

### Unit 3 — JS probe expansion (`assets/probe.js` + `jsprobe.go`)

Fix the dead field, then add device entropy (audio + fonts approved). Every new
field is whitelisted and clamped in `normalizeProbe`, exactly like the existing
`canvas`/`webgl` fields. New `probePayload` fields → components:

| Payload field | Component | Notes |
|---|---|---|
| `hardwareConcurrency` | `js-hardware` | **already collected, currently dropped — the bug fix** ([jsprobe.go:28](web/fingerprint/jsprobe.go:28)) |
| `screen` (`"WxHxdepth"`) | `js-screen` | clamp 20 |
| `devicePixelRatio` | `js-dpr` | clamp 12 |
| `maxTouchPoints` | `js-touch` | int, clamp range 0–256 |
| `deviceMemory` | `js-devicememory` | clamp 8 |
| WebGL **vendor** | `js-webgl-vendor` | today only renderer is captured; clamp 64 |
| `navigator.userAgentData` high-entropy | `js-uadata` | `getHighEntropyValues(['platform','model','architecture','bitness'])`, joined + clamp 128 |
| timezone **offset** (minutes) | `js-tz-offset` | int |
| **AudioContext** hash | `js-audio` | one short hash of an `OfflineAudioContext` render; clamp 64 |
| **font** detection hash | `js-fonts` | hash of the detected subset of a fixed ~40-font probe list, via `measureText` width deltas; clamp 64 — **never the raw list** |

**Payload-size guard (must-have test):** with `WithStore` unset, the whole
payload is base64'd into a signed cookie (~4 KB browser ceiling — see
`readProbe`/`IngestHandler` cookie path). Emitting `js-fonts`/`js-audio` as
fixed-length hashes and clamping every string keeps the cookie path viable. Add
a test that a maximally-filled normalized payload's cookie stays under budget.
`IngestHandler`'s 16 KB `MaxBytesReader` is unaffected.

`getHighEntropyValues` is async (returns a Promise); `probe.js` must await it
before POSTing (restructure the single fire-and-forget POST to run after the
promise resolves, keeping the existing `try/catch` + `keepalive` shape).

### Unit 4 — New signals (`signals_component.go`)

- **`ch-ua-mismatch`** — compares `ch-ua-platform` (`"Windows"`/`"macOS"`/…)
  against `js-platform` (`navigator.platform`: `"Win32"`/`"MacIntel"`/…) through
  a small internal normalization map; disagreement is a spoofing tell. Emits
  only when **both** components are present (no UA-string OS parsing — the
  package extracts no OS from the UA today, and this signal introduces none).
  `Value` is the mismatch boolean.
- **`fetch-metadata-anomaly`** — reads `Sec-Fetch-*` **raw** from the request
  (like `headerAnomaly` already reads `Sec-Fetch-Site`): a top-level document
  navigation (`Sec-Fetch-Dest: document`, `Sec-Fetch-Mode: navigate`) that is
  missing `Sec-Fetch-User`, or self-contradictory combinations, is the tell.
  Emits only when at least one `Sec-Fetch-*` header is present.

Dropped as low-value / redundant: `hardware-anomaly` (weak) and
`tz-offset-mismatch` (overlaps the existing `geo-tz-mismatch`). Existing signals
are unchanged.

### Unit 5 — `automationJA4` made usable

Today `automationJA4` is an empty package-level `var`, so `tls-ua-mismatch` is
permanently inert. Changes:

- Move the map onto the `Fingerprinter` struct (`fp.automationJA4`).
- Add option **`WithAutomationJA4(map[string]string)`** (in `options.go`);
  `tlsUAMismatch` reads `fp.automationJA4`.
- Ship **empty** — no stale/drift-prone data pinned.
- Add a tested **capture recipe** in `doc.go`: how to harvest real JA4s from
  live traffic via the existing `tlsprint.Listener` (`Conn.JA4()`) into the map,
  so a consumer populates the signal from their own traffic.

### Unit 6 — Presets + docs

- `Session()` → append `ClientHints()` (stays pure-stdlib, passive).
- `Antifraud()` → append `ClientHints()`; fold `tlsprint.RequestTLS()` into its
  TLS `Chain(...)` as the self-terminated fallback after the CDN/Local sources.
- Refresh `doc.go`: new collectors, the `WithAutomationJA4` capture recipe, the
  icebox note, and the component-name table.

## Cross-cutting rules

- **Hash-change note.** Wiring `ClientHints()` into the presets changes the
  combined `Fingerprint.Hash` for existing deployments. Because `Drift` compares
  per-component `Parts`, the new component names surface as "changed" exactly
  once on the first post-upgrade comparison, then stabilize. This is a one-time
  churn, documented in `doc.go`; it is **not** gated behind a `Version` bump
  (the consumer owns `Config.Version`).
- **Idiom fidelity.** New collectors follow `headers.go`/`clientip.go`: a small
  struct implementing `Collect`, a constructor returning `Collector`,
  best-effort (never abort the chain), absent input → nothing emitted.
- **Component naming.** Passive HTTP → bare/prefixed header name (`ch-ua-*`);
  connection TLS → `tlsconn`; JS-probe → `js-*`. Consistent with existing
  `ua`/`ip`/`tls`/`js-*`.

## Testing (per `docs/design.md` §Testing)

- Black-box table tests per new collector (`clienthints_test.go`,
  `RequestTLS` in `tlsprint`) and per new signal.
- Extend `jsprobe_fuzz_test.go` to cover the new payload fields through
  `normalizeProbe` (clamping, negative/oversized ints, oversized strings).
- Cookie-size test for the max-fill payload (Unit 3 guard).
- `RequestTLS`: drive via `httptest` with a populated `tls.ConnectionState`;
  assert `tlsconn` value formatting and the `r.TLS == nil` no-op.
- `WithAutomationJA4`: a pinned test map makes `tls-ua-mismatch` fire
  `Value:true` for a matching `tls` component + browser UA.

## File-level change map

- `web/fingerprint/clienthints.go` — **new** (Unit 1)
- `web/fingerprint/clienthints_test.go` — **new**
- `web/fingerprint/tlsprint/request.go` — **new** `RequestTLS()` (Unit 2)
- `web/fingerprint/tlsprint/request_test.go` — **new**
- `web/fingerprint/assets/probe.js` — expanded probe (Unit 3)
- `web/fingerprint/jsprobe.go` — new payload fields, clamps, components (Unit 3)
- `web/fingerprint/jsprobe_fuzz_test.go` — extended (Unit 3)
- `web/fingerprint/signals_component.go` — `ch-ua-mismatch`,
  `fetch-metadata-anomaly`, `fp.automationJA4` read (Units 4, 5)
- `web/fingerprint/options.go` — `WithAutomationJA4` (Unit 5)
- `web/fingerprint/collector.go` — `automationJA4` field on `Fingerprinter` (Unit 5)
- `web/fingerprint/presets.go` — preset wiring (Unit 6)
- `web/fingerprint/doc.go` — docs, capture recipe, icebox note (Unit 6)
- `web/fingerprint/signals_component_test.go`, `presets_test.go` — extended
