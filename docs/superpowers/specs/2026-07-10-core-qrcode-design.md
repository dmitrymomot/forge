# core/qrcode — design

Date: 2026-07-10
Status: approved for planning

## Purpose

Generate QR codes from any string, dependency-free, with a vendored (hand-rolled)
encoder. General-purpose: 2FA/authenticator enrollment (`otpauth://` URIs),
referral/share links, and print/marketing artifacts. Output as a raw module
matrix, PNG, base64 PNG data-URI, or SVG — with configurable size, colors,
center logo, and styled module/eye shapes.

Primary consumer: `auth/totp` (embeds a QR of the enrollment URI). Secondary:
any code turning a link into a scannable image.

## Placement

`core/qrcode` — a stateless free-function brick, same idiom as `core/random`
(free funcs + an internal `config` driven by functional options; no
`New`/`Config`/env tags, no `supervisor.Service`). Zero dependencies: stdlib
only (`image`, `image/color`, `image/png`, `bytes`, `encoding/base64`,
`strings`/`strconv`). When it ships, delete the `core/qrcode` entry from
docs/packages.md.

## Scope decisions

- **Encoder built from the ISO/IEC 18004 spec** (not vendored from a
  third-party library) — truly zero deps, no foreign license header, matches
  forge idioms. Correctness rides on tests (golden vectors + a real decode
  check).
- **Byte mode only** (UTF-8), **automatic version selection 1–40**. Encodes
  every input the use cases have. Numeric/alphanumeric/kanji/ECI modes only
  shrink special inputs (pure digits / uppercase) and add real complexity —
  out of scope.
- **Error-correction levels L/M/Q/H** exposed; **default M**.
- Render surface: **raw `Matrix`** + **PNG** + **PNG data-URI** + **SVG**.
- **Center logo** overlay (PNG + SVG), **styled module + eye shapes** (PNG +
  SVG). Gradients out of scope (solid fg/bg only).

## API surface

```go
package qrcode

// --- error correction ---
type Level int
const (
    LevelL Level = iota // ~7% recovery
    LevelM              // ~15% (default)
    LevelQ              // ~25%
    LevelH              // ~30%
)

// --- module / eye shapes (render-only) ---
type Shape int
const (
    ShapeSquare  Shape = iota // crisp integer blocks (default)
    ShapeRounded              // rounded-corner modules (stay connected)
    ShapeDots                 // detached circles ("designer" look)
)

type EyeShape int
const (
    EyeSquare  EyeShape = iota // default
    EyeRounded                 // rounded finder patterns
)

// --- batteries-included: encode + render in one call ---
func PNG(data string, opts ...Option) ([]byte, error)
func SVG(data string, opts ...Option) ([]byte, error)
func DataURI(data string, opts ...Option) (string, error) // "data:image/png;base64,…"

// --- escape hatch: raw computed grid for custom rendering ---
func Encode(data string, opts ...Option) (*Matrix, error)

type Matrix struct{ /* unexported: modules, size, version, level */ }
func (m *Matrix) Size() int            // modules per side (excludes quiet zone)
func (m *Matrix) Module(x, y int) bool // true = dark
func (m *Matrix) Version() int         // 1–40
func (m *Matrix) Level() Level         // level the grid was encoded at

// --- options (one flat bag) ---
type Option func(*config)
func WithLevel(l Level) Option            // default M
func WithScale(pxPerModule int) Option    // default 8; integer → always crisp
func WithSize(targetPx int) Option        // largest integer scale that fits; wins over WithScale
func WithBorder(modules int) Option       // quiet zone; default 4
func WithForeground(c color.Color) Option // default black
func WithBackground(c color.Color) Option // default white
func WithLogo(img image.Image) Option     // centered; auto-raises level to ≥Q
func WithLogoSize(frac float64) Option    // default 0.2, capped 0.3
func WithModuleShape(s Shape) Option      // default ShapeSquare
func WithEyeShape(e EyeShape) Option       // default EyeSquare
```

- One flat `Option` bag across all funcs. `Encode` is the pure primitive: it
  honors encoding options (`WithLevel`) and **ignores** render-only options
  (`WithScale`/`WithSize`/`WithBorder`/colors/`WithLogo`/shapes) — a raw
  `Matrix` has no size, color, logo, or shape. A custom renderer that wants
  logo/dots robustness sets `WithLevel(Q)` itself.
- `WithLogo`, `WithModuleShape`, `WithEyeShape` affect `PNG`/`SVG`/`DataURI`
  only. The **effective level** used by those renderers is raised to ≥Q when a
  logo or `ShapeDots` is set (see pipeline), guaranteeing a scannable result.
- If both `WithScale` and `WithSize` are set, `WithSize` wins.

## Encoder pipeline (`Encode`)

1. Build + validate `config`, yielding the **effective level**. `Encode` uses
   `WithLevel` (default M) as-is. The renderers (`PNG`/`SVG`/`DataURI`) raise
   it to `max(level, Q)` when a logo or `ShapeDots` is set, then run this same
   pipeline. `Matrix.Level()` reports whatever level the grid was encoded at.
2. Pick the smallest version whose byte-mode data capacity at the effective
   level fits `len([]byte(data))`; `ErrTooLarge` if it won't fit at v40.
3. Build the bitstream: mode indicator `0100` + character-count indicator
   (width per version group) + data bytes + terminator + pad to a codeword
   boundary + pad codewords (`0xEC`, `0x11` alternating).
4. Split data codewords into EC blocks per the version/level table; compute
   Reed-Solomon EC codewords per block over GF(256); interleave data then EC
   codewords, append remainder bits.
5. Assemble the matrix: finder patterns + separators + timing patterns +
   alignment patterns + dark module; reserve format/version-info areas; place
   the interleaved bitstream in the standard zigzag order, skipping function
   modules.
6. Evaluate all 8 mask patterns, score each with the four penalty rules, apply
   the lowest-penalty mask; write format info (level + mask, BCH) and, for
   v ≥ 7, version info. → `*Matrix` (modules only; no quiet zone).

## Rendering

Shared: resolve scale (`WithSize` → largest integer scale that fits its target,
else `WithScale`); add the quiet-zone border (default 4 modules) around the
grid; foreground for dark modules, background for the rest.

- **PNG / DataURI** — draw into an `*image.RGBA`, then `png.Encode`; `DataURI`
  base64-wraps the PNG bytes as `data:image/png;base64,…`.
  - `ShapeSquare` + `EyeSquare`: integer-scaled blocks, no supersampling —
    perfectly crisp.
  - `ShapeRounded`/`ShapeDots` or `EyeRounded`: **supersample** — render the
    shaped grid at ~4× into an RGBA buffer, then box-downsample (hand-rolled
    averaging, ~40 LOC, no dep) so curved edges get anti-aliased. Cold path,
    so the extra work is acceptable.
- **SVG** — background `<rect>` + a `<path>`/`<rect>`/`<circle>` set for dark
  modules (shape-dependent); eyes emitted per `EyeShape`. Vector, so shapes are
  exact with no supersampling.
- **Logo** (both formats) — scale the caller-decoded `image.Image` to
  `WithLogoSize` × code width (default 0.2, cap 0.3), draw a small white
  backing pad, composite centered. PNG composites into the raster; SVG embeds
  the logo as a base64 `<image>` element.

## Errors

`errors.Is`-matchable, single-line, package-prefixed sentinels in `errors.go`:

- `ErrTooLarge` — data exceeds byte-mode capacity even at v40 for the effective
  level. The key runtime error.
- Config validation returns descriptive errors for programmer mistakes:
  `WithScale ≤ 0`, `WithBorder < 0`, `WithLogoSize` outside `(0, 0.3]`.

Empty input is legal (an empty byte-mode symbol). No panics on caller input.

## Files (forge anatomy)

`doc.go` (runnable example) · `config.go` (`config` + `defaultConfig` +
`validate`) · `options.go` (`Option func(*config)`) · `errors.go` · `level.go`
(`Level` + `String`) · `encode.go` (byte-mode bitstream) · `reedsolomon.go`
(GF(256) arithmetic + EC codewords + block interleaving) · `tables.go`
(version/capacity/EC-block/alignment/BCH tables) · `matrix.go` (function
patterns + data placement + `Matrix` accessors) · `mask.go` (8 masks + penalty
scoring) · `render_png.go` (+ DataURI + supersampling) · `render_svg.go` ·
`logo.go` (shared compositing) · `shape.go` (`Shape`/`EyeShape` + geometry).

**LOC note:** a spec-complete encoder plus its version tables runs ~900–1200
LOC — above the ~850 soft band in design.md. Justified exception (same as
`validate`/`decimal`): the tables are data, and "QR generation" is genuinely
one responsibility. Do **not** fracture the package to hit the band.

## Testing (black-box `qrcode_test`)

- **Golden module matrices** — the real correctness guarantee. Encode fixed
  inputs at each level, assert the exact grid against reference vectors in
  `testdata` (cross-checked against an authoritative encoder when generated).
  Proves the hand-rolled Reed-Solomon and masking are correct.
- **Golden PNG/SVG snapshots** via `testkit/golden` (`-update`), plus a
  documented one-time manual phone-scan during development.
- Edge / property tests: capacity boundaries (largest fit at vN, +1 byte bumps
  version or errors at v40), all four levels, version auto-selection, logo
  auto-raises level to Q, `ShapeDots` auto-raises level to Q, `WithSize` →
  expected integer scale, fg/bg reflected in output pixels, quiet-zone width,
  `WithLogoSize` cap enforced.
### Benchmarks (`bench_test.go`, `b.ReportAllocs()`)

**Perf is a cold path** — codes are generated at enrollment / link creation,
not per request — so design.md's meta-rule holds: readable first, no zero-alloc
gymnastics. Benchmarks here **establish a baseline and guard against regressions**
(especially the deliberately-expensive supersampling path), not to chase
micro-optimizations. Per design.md, the supersampling render is perf-motivated
complexity, so its benchmark is what justifies it in the PR.

Cover, at minimum:

- `BenchmarkEncode` — pure pipeline (RS + 8-mask penalty scoring is the cost),
  at a **short input** (an `otpauth://` URI, ~v3) and a **large input** (a long
  URL near v10+ capacity), so version scaling is visible.
- `BenchmarkEncode` across the four EC levels at a fixed input (EC codeword
  volume grows L→H).
- `BenchmarkPNG_Square` — crisp integer-scaled raster (no supersampling); the
  common 2FA/embed path and the baseline.
- `BenchmarkPNG_Shaped` — `ShapeRounded`/`ShapeDots` supersampled path; the
  4×-render + downsample cost measured against `BenchmarkPNG_Square` to quantify
  the trade.
- `BenchmarkPNG_Logo` — logo scale + composite overhead.
- `BenchmarkSVG` — encode + string build (path assembly), short and large input.

Report ns/op and allocs/op; treat large regressions in the square/SVG paths as
signals, and keep the shaped-vs-square multiple documented as the expected cost.

## Consumers & integration

- **`auth/totp`** — `qrcode.DataURI(otpauthURI)` embedded in the enrollment
  page `<img src>`; or `PNG` for download.
- **Referral / share / marketing** — `PNG`/`SVG` with `WithLogo` + `WithLevel(Q|H)`
  and optional shaped modules/eyes.

## Non-goals (YAGNI)

Numeric/alphanumeric/kanji/ECI modes · structured-append · micro-QR · QR
*decoding/reading* · color gradients · animated output. Byte mode + the render
options above cover every stated use case.
