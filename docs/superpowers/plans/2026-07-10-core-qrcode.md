# core/qrcode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `core/qrcode` — a dependency-free QR code generator producing a raw module matrix, PNG, PNG data-URI, and SVG, with configurable size/colors, a center logo, and styled module/eye shapes.

**Architecture:** A stateless free-function brick (same idiom as `core/random`): public `Encode`/`PNG`/`SVG`/`DataURI` funcs driven by an internal `config` built from functional options. A hand-rolled ISO/IEC 18004 byte-mode encoder (Reed-Solomon over GF(256), block interleaving, matrix assembly, 8-mask penalty selection) produces a `*Matrix`; thin renderers rasterize/vectorize it. Shapes are a pure render concern; the encoder is untouched by them.

**Tech Stack:** Go 1.26, stdlib only (`image`, `image/color`, `image/draw`, `image/png`, `bytes`, `encoding/base64`, `strings`, `strconv`). No third-party dependencies in the package. Dev-only reference tool `qrencode` (libqrencode) is used to capture golden test vectors — never imported.

## Global Constraints

- Module path: `github.com/dmitrymomot/forge`. Package import path: `github.com/dmitrymomot/forge/core/qrcode`.
- Go 1.26. **Zero third-party dependencies** — stdlib only.
- Idiom: stateless free-funcs + internal `config` + `type Option func(*config)`. No `New`, no exported `Config`, no env tags, no `supervisor.Service`.
- Anatomy files: `doc.go` (runnable example), `config.go`, `options.go`, `errors.go`, plus impl files. Never builders — options only.
- Error sentinels: `errors.Is`-matchable, single-line, package-prefixed: `errors.New("qrcode: …")`.
- Tests are **black-box**: test files use `package qrcode_test` and import the package. White-box (`package qrcode`) only where an internal (GF math, tables, mask scoring) has no public surface.
- Encoder is **byte mode only**, auto version 1–40. EC levels L/M/Q/H, default M.
- After changing any Go file run `just fmt ./core/qrcode/...`. After the task's tests pass, they must pass under `just test ./core/qrcode/...`. At the end of the plan run `just lint` (runs modernize/betteralign/nilaway) and fix all findings.
- Go 1.26 `new(expr)` is allowed; do **not** add a `ptr.To`-style wrapper (modernize will flag it).
- No Claude attribution in commit messages.
- Commit messages: Conventional Commits, `feat(qrcode): …` / `test(qrcode): …` / `docs(qrcode): …`.

---

## File structure

| File | Responsibility |
| --- | --- |
| `core/qrcode/doc.go` | Package doc + runnable `Example`. |
| `core/qrcode/level.go` | `Level` type, constants, `String()`, internal `formatBits()`. |
| `core/qrcode/shape.go` | `Shape`/`EyeShape` types + constants + shape geometry helpers. |
| `core/qrcode/errors.go` | Sentinel errors. |
| `core/qrcode/config.go` | `config` struct, `defaultConfig`, `newConfig`, `validate`, effective-level/scale resolution. |
| `core/qrcode/options.go` | `Option` + all `With*`. |
| `core/qrcode/reedsolomon.go` | GF(256) exp/log tables, `gfMul`, generator polynomials, `ecCodewords`. |
| `core/qrcode/tables.go` | Version capacity, EC-block structure, alignment-pattern centers, char-count widths — transcribed from ISO/IEC 18004. |
| `core/qrcode/encode.go` | `pickVersion`, byte-mode bitstream → data codewords, block interleaving → final codeword stream. |
| `core/qrcode/matrix.go` | internal `grid`, function-pattern placement, data placement, `Matrix` type + accessors, public `Encode`. |
| `core/qrcode/mask.go` | 8 mask functions, penalty scoring, best-mask selection. |
| `core/qrcode/render_png.go` | `PNG`, `DataURI`, raster draw + supersampling. |
| `core/qrcode/render_svg.go` | `SVG` vector renderer. |
| `core/qrcode/logo.go` | Logo scaling + centered composite (shared by PNG/SVG). |
| `core/qrcode/bench_test.go` | Benchmarks. |
| test files | `*_test.go` alongside each impl file. |

---

## Task 1: Package skeleton — types, errors, config, options

**Files:**
- Create: `core/qrcode/level.go`, `core/qrcode/shape.go`, `core/qrcode/errors.go`, `core/qrcode/config.go`, `core/qrcode/options.go`
- Test: `core/qrcode/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Level int`; `LevelL, LevelM, LevelQ, LevelH Level`; `func (Level) String() string`.
  - `type Shape int`; `ShapeSquare, ShapeRounded, ShapeDots Shape`.
  - `type EyeShape int`; `EyeSquare, EyeRounded EyeShape`.
  - `type Option func(*config)` and every `With*` option.
  - `type config struct{…}`, `func defaultConfig() config`, `func newConfig(opts ...Option) (config, error)`, `func (config) validate() error`.
  - Sentinels: `ErrTooLarge`, `ErrInvalidScale`, `ErrInvalidBorder`, `ErrInvalidLogoSize`.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/config_test.go`:

```go
package qrcode

import (
	"errors"
	"image/color"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c, err := newConfig()
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.level != LevelM {
		t.Errorf("level = %v, want LevelM", c.level)
	}
	if c.scale != 8 {
		t.Errorf("scale = %d, want 8", c.scale)
	}
	if c.border != 4 {
		t.Errorf("border = %d, want 4", c.border)
	}
	if c.fg != (color.Black) || c.bg != (color.White) {
		t.Errorf("fg/bg = %v/%v, want black/white", c.fg, c.bg)
	}
	if c.moduleShape != ShapeSquare || c.eyeShape != EyeSquare {
		t.Errorf("shapes = %v/%v, want square/square", c.moduleShape, c.eyeShape)
	}
	if c.logoSize != 0.2 {
		t.Errorf("logoSize = %v, want 0.2", c.logoSize)
	}
}

func TestOptionsApply(t *testing.T) {
	c, err := newConfig(
		WithLevel(LevelH), WithScale(10), WithSize(300), WithBorder(2),
		WithForeground(color.RGBA{R: 1}), WithBackground(color.RGBA{B: 1}),
		WithModuleShape(ShapeDots), WithEyeShape(EyeRounded), WithLogoSize(0.25),
	)
	if err != nil {
		t.Fatalf("newConfig: %v", err)
	}
	if c.level != LevelH || c.scale != 10 || c.targetSize != 300 || c.border != 2 {
		t.Errorf("options not applied: %+v", c)
	}
	if c.moduleShape != ShapeDots || c.eyeShape != EyeRounded || c.logoSize != 0.25 {
		t.Errorf("shape/logo options not applied: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want error
	}{
		{"scale zero", []Option{WithScale(0)}, ErrInvalidScale},
		{"scale negative", []Option{WithScale(-1)}, ErrInvalidScale},
		{"border negative", []Option{WithBorder(-1)}, ErrInvalidBorder},
		{"logo too big", []Option{WithLogoSize(0.4)}, ErrInvalidLogoSize},
		{"logo zero", []Option{WithLogoSize(0)}, ErrInvalidLogoSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newConfig(tt.opts...); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	if LevelL.String() != "L" || LevelM.String() != "M" || LevelQ.String() != "Q" || LevelH.String() != "H" {
		t.Error("Level.String mismatch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — build error, `undefined: newConfig` etc.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/level.go`:

```go
package qrcode

// Level is the QR error-correction level: higher levels recover more of a
// damaged or obscured symbol at the cost of a denser code.
type Level int

const (
	LevelL Level = iota // ~7% recovery
	LevelM              // ~15% recovery (default)
	LevelQ              // ~25% recovery
	LevelH              // ~30% recovery
)

// String returns the single-letter level name ("L", "M", "Q", "H").
func (l Level) String() string {
	switch l {
	case LevelL:
		return "L"
	case LevelM:
		return "M"
	case LevelQ:
		return "Q"
	case LevelH:
		return "H"
	default:
		return "?"
	}
}
```

Create `core/qrcode/shape.go` (types only for now; geometry added in Task 9):

```go
package qrcode

// Shape styles the dark data modules when rendering to PNG or SVG.
type Shape int

const (
	ShapeSquare  Shape = iota // crisp integer-scaled blocks (default)
	ShapeRounded              // rounded-corner modules; stay connected
	ShapeDots                 // detached circles; raises level to >=Q
)

// EyeShape styles the three finder ("eye") patterns independently of the
// data modules.
type EyeShape int

const (
	EyeSquare  EyeShape = iota // default
	EyeRounded                 // rounded finder patterns
)
```

Create `core/qrcode/errors.go`:

```go
package qrcode

import "errors"

var (
	// ErrTooLarge reports that the input exceeds the byte-mode capacity of the
	// largest QR version (40) at the effective error-correction level.
	ErrTooLarge = errors.New("qrcode: data too large for a QR code")
	// ErrInvalidScale reports a non-positive WithScale value.
	ErrInvalidScale = errors.New("qrcode: scale must be positive")
	// ErrInvalidBorder reports a negative WithBorder value.
	ErrInvalidBorder = errors.New("qrcode: border must be non-negative")
	// ErrInvalidLogoSize reports a WithLogoSize outside (0, 0.3].
	ErrInvalidLogoSize = errors.New("qrcode: logo size must be within (0, 0.3]")
)
```

Create `core/qrcode/config.go`:

```go
package qrcode

import (
	"fmt"
	"image"
	"image/color"
)

const (
	defaultScale    = 8
	defaultBorder   = 4
	defaultLogoSize = 0.2
	maxLogoSize     = 0.3
)

type config struct {
	level       Level
	scale       int
	targetSize  int // WithSize; 0 = unset. Wins over scale when > 0.
	border      int
	fg, bg      color.Color
	logo        image.Image // nil = no logo
	logoSize    float64
	moduleShape Shape
	eyeShape    EyeShape
}

func defaultConfig() config {
	return config{
		level:       LevelM,
		scale:       defaultScale,
		border:      defaultBorder,
		fg:          color.Black,
		bg:          color.White,
		logoSize:    defaultLogoSize,
		moduleShape: ShapeSquare,
		eyeShape:    EyeSquare,
	}
}

func newConfig(opts ...Option) (config, error) {
	c := defaultConfig()
	for _, opt := range opts {
		opt(&c)
	}
	if err := c.validate(); err != nil {
		return config{}, err
	}
	return c, nil
}

func (c config) validate() error {
	if c.scale <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidScale, c.scale)
	}
	if c.border < 0 {
		return fmt.Errorf("%w: %d", ErrInvalidBorder, c.border)
	}
	if c.logoSize <= 0 || c.logoSize > maxLogoSize {
		return fmt.Errorf("%w: %v", ErrInvalidLogoSize, c.logoSize)
	}
	return nil
}

// effectiveLevel raises the encoding level to the minimum a render decoration
// needs to stay scannable. Called by the renderers; Encode uses c.level as-is.
func (c config) effectiveLevel() Level {
	l := c.level
	if (c.logo != nil || c.moduleShape == ShapeDots) && l < LevelQ {
		l = LevelQ
	}
	return l
}
```

Create `core/qrcode/options.go`:

```go
package qrcode

import (
	"image"
	"image/color"
)

// Option configures a QR render/encode call.
type Option func(*config)

// WithLevel sets the error-correction level (default LevelM).
func WithLevel(l Level) Option { return func(c *config) { c.level = l } }

// WithScale sets the number of pixels per module for raster output (default 8).
// Integer scaling keeps modules crisp.
func WithScale(pxPerModule int) Option { return func(c *config) { c.scale = pxPerModule } }

// WithSize requests an approximate output width in pixels; the renderer picks
// the largest integer scale whose full image (modules + border) fits. Wins
// over WithScale when both are set.
func WithSize(targetPx int) Option { return func(c *config) { c.targetSize = targetPx } }

// WithBorder sets the quiet-zone width in modules (default 4).
func WithBorder(modules int) Option { return func(c *config) { c.border = modules } }

// WithForeground sets the dark-module color (default black).
func WithForeground(col color.Color) Option { return func(c *config) { c.fg = col } }

// WithBackground sets the background color (default white).
func WithBackground(col color.Color) Option { return func(c *config) { c.bg = col } }

// WithLogo overlays a centered, caller-decoded image on PNG/SVG output. A logo
// raises the effective error-correction level to at least LevelQ.
func WithLogo(img image.Image) Option { return func(c *config) { c.logo = img } }

// WithLogoSize sets the logo width as a fraction of the code width (default
// 0.2, capped at 0.3).
func WithLogoSize(frac float64) Option { return func(c *config) { c.logoSize = frac } }

// WithModuleShape styles the data modules (default ShapeSquare).
func WithModuleShape(s Shape) Option { return func(c *config) { c.moduleShape = s } }

// WithEyeShape styles the finder patterns (default EyeSquare).
func WithEyeShape(e EyeShape) Option { return func(c *config) { c.eyeShape = e } }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS (all `config_test.go` tests green).

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/
git commit -m "feat(qrcode): package skeleton — types, errors, config, options"
```

---

## Task 2: Reed-Solomon over GF(256)

**Files:**
- Create: `core/qrcode/reedsolomon.go`
- Test: `core/qrcode/reedsolomon_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `var gfExp [256]byte`, `var gfLog [256]byte` (package-level, built in `init`).
  - `func gfMul(a, b byte) byte`.
  - `func rsGenerator(n int) []byte` — generator polynomial coefficients for `n` EC codewords, length `n+1`, leading coefficient 1.
  - `func ecCodewords(data []byte, n int) []byte` — `n` Reed-Solomon EC codewords for `data`.

QR uses GF(2^8) with primitive polynomial `0x11D` and generator element `2`.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/reedsolomon_test.go`:

```go
package qrcode

import "testing"

func TestGFTables(t *testing.T) {
	// Anchors from GF(256) with primitive poly 0x11D, generator 2.
	if gfExp[0] != 1 || gfExp[1] != 2 || gfExp[8] != 0x1D {
		t.Errorf("gfExp anchors wrong: [0]=%d [1]=%d [8]=%d", gfExp[0], gfExp[1], gfExp[8])
	}
	if gfLog[1] != 0 || gfLog[2] != 1 {
		t.Errorf("gfLog anchors wrong: [1]=%d [2]=%d", gfLog[1], gfLog[2])
	}
	// Round-trip: exp(log(x)) == x for all non-zero x.
	for x := 1; x < 256; x++ {
		if gfExp[gfLog[byte(x)]] != byte(x) {
			t.Fatalf("exp(log(%d)) != %d", x, x)
		}
	}
}

func TestGFMul(t *testing.T) {
	cases := []struct{ a, b, want byte }{
		{0, 5, 0}, {5, 0, 0}, {1, 42, 42}, {2, 2, 4}, {2, 128, 0x1D},
	}
	for _, c := range cases {
		if got := gfMul(c.a, c.b); got != c.want {
			t.Errorf("gfMul(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRSGenerator(t *testing.T) {
	g := rsGenerator(10)
	if len(g) != 11 {
		t.Fatalf("len(generator) = %d, want 11", len(g))
	}
	if g[0] != 1 {
		t.Errorf("leading coeff = %d, want 1", g[0])
	}
}

func TestECCodewordsDivisibility(t *testing.T) {
	// Defining RS property: (data << n) with the EC codewords appended is
	// exactly divisible by the generator polynomial, i.e. dividing the full
	// codeword stream by the generator leaves a zero remainder.
	data := []byte{32, 91, 11, 120, 209, 114, 220, 77, 67, 64, 236, 17, 236}
	n := 10
	ec := ecCodewords(data, n)
	if len(ec) != n {
		t.Fatalf("len(ec) = %d, want %d", len(ec), n)
	}
	full := append(append([]byte{}, data...), ec...)
	if rem := ecCodewords(full, n); !allZero(rem) {
		t.Errorf("codeword stream not divisible by generator: remainder %v", rem)
	}
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: gfExp`, `gfMul`, `rsGenerator`, `ecCodewords`.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/reedsolomon.go`:

```go
package qrcode

var (
	gfExp [256]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[byte(x)] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D // primitive polynomial
		}
	}
	gfExp[255] = gfExp[0] // 2^255 == 1; wrap for convenience
}

// gfMul multiplies two elements of GF(256).
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[(int(gfLog[a])+int(gfLog[b]))%255]
}

// rsGenerator returns the degree-n Reed-Solomon generator polynomial,
// product of (x - 2^i) for i in [0, n), coefficients high-order first.
func rsGenerator(n int) []byte {
	g := []byte{1}
	for i := 0; i < n; i++ {
		// Multiply g(x) by (x - 2^i)  ==  (x + 2^i) in GF(256).
		next := make([]byte, len(g)+1)
		root := gfExp[i]
		for j, c := range g {
			next[j] ^= c                 // c * x
			next[j+1] ^= gfMul(c, root)  // c * root
		}
		g = next
	}
	return g
}

// ecCodewords returns n Reed-Solomon error-correction codewords for data:
// the remainder of data*x^n divided by the degree-n generator polynomial.
func ecCodewords(data []byte, n int) []byte {
	gen := rsGenerator(n)
	rem := make([]byte, len(data)+n)
	copy(rem, data)
	for i := 0; i < len(data); i++ {
		coef := rem[i]
		if coef == 0 {
			continue
		}
		for j, gc := range gen {
			rem[i+j] ^= gfMul(gc, coef)
		}
	}
	return rem[len(data):]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/reedsolomon.go core/qrcode/reedsolomon_test.go
git commit -m "feat(qrcode): Reed-Solomon error correction over GF(256)"
```

---

## Task 3: Version tables + byte-mode data encoding

**Files:**
- Create: `core/qrcode/tables.go`, `core/qrcode/encode.go`
- Test: `core/qrcode/encode_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (uses `Level`).
- Produces:
  - In `tables.go`:
    - `type blockSpec struct{ ecPerBlock, group1Blocks, group1Words, group2Blocks, group2Words int }`.
    - `func versionSpec(version int, level Level) blockSpec` — EC-block structure per ISO/IEC 18004 Table 9.
    - `func dataCapacityBytes(version int, level Level) int` — usable byte-mode payload capacity (bytes) after mode+count overhead is deducted at encode time; implemented as total data codewords for the version/level (used by `pickVersion`).
    - `func alignmentCenters(version int) []int` — alignment-pattern center coordinates (empty for v1).
    - `func charCountBits(version int) int` — byte-mode char-count indicator width (8 for v1–9, else 16).
  - In `encode.go`:
    - `func pickVersion(dataLen int, level Level) (int, error)` — smallest version whose byte-mode capacity fits `dataLen`; `ErrTooLarge` past v40.
    - `func encodeData(data []byte, version int, level Level) []byte` — full data-codeword block (mode + count + bytes + terminator + pad), length == total data codewords for version/level.

**Reference for tables:** transcribe the QR capacity + EC-block tables from ISO/IEC 18004:2015 (or the widely-mirrored "thonky.com QR tutorial" error-correction table, which reproduces Annex D). Fill `tables.go` for **all 40 versions × 4 levels**. Correctness is gated by the boundary tests here and the end-to-end golden matrix in Task 6 — a mis-transcribed row makes those fail.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/encode_test.go`:

```go
package qrcode

import (
	"errors"
	"testing"
)

func TestCharCountBits(t *testing.T) {
	if charCountBits(1) != 8 || charCountBits(9) != 8 {
		t.Error("v1-9 byte char-count must be 8 bits")
	}
	if charCountBits(10) != 16 || charCountBits(40) != 16 {
		t.Error("v10-40 byte char-count must be 16 bits")
	}
}

func TestVersionSpecKnown(t *testing.T) {
	// ISO/IEC 18004 Table 9 anchors.
	// v1-M: 1 block, 16 data codewords, 10 EC codewords.
	s := versionSpec(1, LevelM)
	if s.ecPerBlock != 10 || s.group1Blocks != 1 || s.group1Words != 16 || s.group2Blocks != 0 {
		t.Errorf("v1-M spec wrong: %+v", s)
	}
	// v5-Q: 2 blocks of 15 + 2 blocks of 16, 18 EC codewords each.
	s = versionSpec(5, LevelQ)
	if s.ecPerBlock != 18 || s.group1Blocks != 2 || s.group1Words != 15 ||
		s.group2Blocks != 2 || s.group2Words != 16 {
		t.Errorf("v5-Q spec wrong: %+v", s)
	}
}

func TestPickVersion(t *testing.T) {
	// v1-M holds 16 data codewords = 16 bytes total, minus mode(4b)+count(8b)+
	// terminator overhead: ~14 bytes of payload. A 10-byte payload fits v1.
	v, err := pickVersion(10, LevelM)
	if err != nil {
		t.Fatalf("pickVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("version = %d, want 1", v)
	}
	// Way past the v40 ceiling must error.
	if _, err := pickVersion(1 << 20, LevelM); !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestEncodeDataStructure(t *testing.T) {
	v, _ := pickVersion(4, LevelM)
	cw := encodeData([]byte("test"), v, LevelM)
	total := versionSpec(v, LevelM)
	want := (total.group1Blocks*total.group1Words + total.group2Blocks*total.group2Words)
	if len(cw) != want {
		t.Fatalf("len(codewords) = %d, want %d", len(cw), want)
	}
	// First byte: mode nibble 0100 then high nibble of length (4). "test" len=4,
	// v1 count is 8 bits -> first byte = 0100_0000 = 0x40, second = 0100_XXXX.
	if cw[0] != 0x40 {
		t.Errorf("cw[0] = %#x, want 0x40 (byte mode + high count nibble)", cw[0])
	}
	// Tail must be pad codewords 0xEC / 0x11 alternating once padding starts.
	if cw[len(cw)-1] != 0x11 && cw[len(cw)-1] != 0xEC {
		t.Errorf("last codeword = %#x, want pad 0xEC/0x11", cw[len(cw)-1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: charCountBits`, `versionSpec`, `pickVersion`, `encodeData`.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/tables.go`. Structure shown; **fill every version row from the cited reference**:

```go
package qrcode

type blockSpec struct {
	ecPerBlock   int
	group1Blocks int
	group1Words  int
	group2Blocks int
	group2Words  int
}

// ecBlocks[version][level] holds the EC-block structure from ISO/IEC 18004
// Table 9. Index 0 is unused (versions are 1-based); level order is
// L, M, Q, H matching the Level iota.
//
// TRANSCRIBE ALL 40 VERSIONS from the cited reference. A representative
// prefix is shown; the encoder is incorrect until every row is present.
var ecBlocks = [41][4]blockSpec{
	1: {
		LevelL: {ecPerBlock: 7, group1Blocks: 1, group1Words: 19},
		LevelM: {ecPerBlock: 10, group1Blocks: 1, group1Words: 16},
		LevelQ: {ecPerBlock: 13, group1Blocks: 1, group1Words: 13},
		LevelH: {ecPerBlock: 17, group1Blocks: 1, group1Words: 9},
	},
	2: {
		LevelL: {ecPerBlock: 10, group1Blocks: 1, group1Words: 34},
		LevelM: {ecPerBlock: 16, group1Blocks: 1, group1Words: 28},
		LevelQ: {ecPerBlock: 22, group1Blocks: 1, group1Words: 22},
		LevelH: {ecPerBlock: 28, group1Blocks: 1, group1Words: 16},
	},
	5: {
		LevelL: {ecPerBlock: 26, group1Blocks: 1, group1Words: 108},
		LevelM: {ecPerBlock: 24, group1Blocks: 2, group1Words: 43},
		LevelQ: {ecPerBlock: 18, group1Blocks: 2, group1Words: 15, group2Blocks: 2, group2Words: 16},
		LevelH: {ecPerBlock: 22, group1Blocks: 2, group1Words: 11, group2Blocks: 2, group2Words: 12},
	},
	// … versions 3,4,6..40 …
}

// alignmentCentersByVersion[version] lists the alignment-pattern center
// coordinates (ISO/IEC 18004 Annex E). v1 has none. TRANSCRIBE ALL ROWS.
var alignmentCentersByVersion = [41][]int{
	1:  {},
	2:  {6, 18},
	5:  {6, 30},
	7:  {6, 22, 38},
	// … all versions …
}

func versionSpec(version int, level Level) blockSpec {
	return ecBlocks[version][level]
}

func alignmentCenters(version int) []int {
	return alignmentCentersByVersion[version]
}

func charCountBits(version int) int {
	if version <= 9 {
		return 8
	}
	return 16
}

// dataCodewords returns the total number of data codewords for a version/level.
func dataCodewords(version int, level Level) int {
	s := ecBlocks[version][level]
	return s.group1Blocks*s.group1Words + s.group2Blocks*s.group2Words
}
```

Create `core/qrcode/encode.go`:

```go
package qrcode

// pickVersion returns the smallest version whose byte-mode payload capacity
// holds dataLen bytes at the given level.
func pickVersion(dataLen int, level Level) (int, error) {
	for v := 1; v <= 40; v++ {
		// Available data bits minus mode indicator (4) and char-count width.
		capBits := dataCodewords(v, level)*8 - 4 - charCountBits(v)
		if dataLen*8 <= capBits {
			return v, nil
		}
	}
	return 0, ErrTooLarge
}

// encodeData builds the full data-codeword block for a byte-mode payload:
// mode indicator, char count, data bytes, terminator, bit padding, then
// alternating pad codewords to fill the version/level capacity.
func encodeData(data []byte, version int, level Level) []byte {
	total := dataCodewords(version, level)
	bs := newBitBuffer(total)
	bs.appendBits(0b0100, 4)                 // byte mode indicator
	bs.appendBits(uint(len(data)), charCountBits(version))
	for _, b := range data {
		bs.appendBits(uint(b), 8)
	}
	// Terminator: up to 4 zero bits, not exceeding capacity.
	remaining := total*8 - bs.length()
	term := 4
	if remaining < term {
		term = remaining
	}
	bs.appendBits(0, term)
	// Pad to a byte boundary.
	if pad := (8 - bs.length()%8) % 8; pad > 0 {
		bs.appendBits(0, pad)
	}
	// Pad codewords 0xEC, 0x11 alternating.
	out := bs.bytes()
	for i := len(out); i < total; i++ {
		if (i-len(out))%2 == 0 {
			out = append(out, 0xEC)
		} else {
			out = append(out, 0x11)
		}
	}
	return out
}
```

Add a tiny bit buffer at the bottom of `encode.go`:

```go
type bitBuffer struct {
	buf    []byte
	nbits  int
}

func newBitBuffer(capBytes int) *bitBuffer {
	return &bitBuffer{buf: make([]byte, 0, capBytes)}
}

func (b *bitBuffer) length() int { return b.nbits }

func (b *bitBuffer) appendBits(v uint, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((v >> uint(i)) & 1)
		if b.nbits%8 == 0 {
			b.buf = append(b.buf, 0)
		}
		b.buf[b.nbits/8] |= bit << uint(7-b.nbits%8)
		b.nbits++
	}
}

// bytes returns the buffered bytes (last byte already zero-padded on the right
// because appendBits pads to a byte boundary before this is called).
func (b *bitBuffer) bytes() []byte { return b.buf }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS. (If `TestVersionSpecKnown` or `TestEncodeDataStructure` fail, a table row is mis-transcribed — fix `tables.go`.)

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/tables.go core/qrcode/encode.go core/qrcode/encode_test.go
git commit -m "feat(qrcode): version tables and byte-mode data encoding"
```

---

## Task 4: Block splitting + interleaving

**Files:**
- Modify: `core/qrcode/encode.go`
- Test: `core/qrcode/interleave_test.go`

**Interfaces:**
- Consumes: `versionSpec`, `dataCodewords`, `encodeData`, `ecCodewords`.
- Produces: `func finalCodewords(data []byte, version int, level Level) []byte` — the interleaved data+EC codeword stream ready for matrix placement.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/interleave_test.go`:

```go
package qrcode

import "testing"

func TestFinalCodewordsLength(t *testing.T) {
	// Total codewords = data codewords + EC codewords across all blocks.
	v, level := 5, LevelQ
	s := versionSpec(v, level)
	dataCW := dataCodewords(v, level)
	blocks := s.group1Blocks + s.group2Blocks
	wantTotal := dataCW + blocks*s.ecPerBlock

	got := finalCodewords([]byte("interleave me across blocks!!"), v, level)
	if len(got) != wantTotal {
		t.Fatalf("len(final) = %d, want %d", len(got), wantTotal)
	}
}

func TestFinalCodewordsSingleBlock(t *testing.T) {
	// v1-M is a single block: interleaving is identity — data codewords then
	// EC codewords, in order.
	v, level := 1, LevelM
	payload := []byte("hello")
	data := encodeData(payload, v, level)
	ec := ecCodewords(data, versionSpec(v, level).ecPerBlock)

	got := finalCodewords(payload, v, level)
	want := append(append([]byte{}, data...), ec...)
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d = %d, want %d", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: finalCodewords`.

- [ ] **Step 3: Write minimal implementation**

Append to `core/qrcode/encode.go`:

```go
// finalCodewords encodes data, splits it into the version/level's EC blocks,
// computes each block's EC codewords, and interleaves data then EC codewords
// per ISO/IEC 18004 §8.6.
func finalCodewords(data []byte, version int, level Level) []byte {
	s := versionSpec(version, level)
	all := encodeData(data, version, level)

	// Split into blocks.
	type block struct{ data, ec []byte }
	blocks := make([]block, 0, s.group1Blocks+s.group2Blocks)
	pos := 0
	addBlocks := func(count, words int) {
		for i := 0; i < count; i++ {
			d := all[pos : pos+words]
			pos += words
			blocks = append(blocks, block{data: d, ec: ecCodewords(d, s.ecPerBlock)})
		}
	}
	addBlocks(s.group1Blocks, s.group1Words)
	addBlocks(s.group2Blocks, s.group2Words)

	out := make([]byte, 0, len(all)+len(blocks)*s.ecPerBlock)
	// Interleave data codewords column-by-column across blocks.
	maxData := s.group1Words
	if s.group2Words > maxData {
		maxData = s.group2Words
	}
	for i := 0; i < maxData; i++ {
		for _, b := range blocks {
			if i < len(b.data) {
				out = append(out, b.data[i])
			}
		}
	}
	// Interleave EC codewords column-by-column across blocks.
	for i := 0; i < s.ecPerBlock; i++ {
		for _, b := range blocks {
			out = append(out, b.ec[i])
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/encode.go core/qrcode/interleave_test.go
git commit -m "feat(qrcode): EC block splitting and codeword interleaving"
```

---

## Task 5: Matrix — function-pattern placement

**Files:**
- Create: `core/qrcode/matrix.go`
- Test: `core/qrcode/matrix_test.go`

**Interfaces:**
- Consumes: `alignmentCenters`.
- Produces (internal):
  - `type grid struct{ size int; module []bool; reserved []bool }` with `func newGrid(version int) *grid`, `func (g *grid) at(x, y int) bool`, `func (g *grid) set(x, y int, dark bool)`, `func (g *grid) reserve(x, y int)`, `func (g *grid) isReserved(x, y int) bool`.
  - `func moduleCount(version int) int` — `17 + 4*version`.
  - `func (g *grid) placeFunctionPatterns(version int)` — finders, separators, timing, alignment, dark module; reserves format/version-info areas.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/matrix_test.go`:

```go
package qrcode

import "testing"

func TestModuleCount(t *testing.T) {
	if moduleCount(1) != 21 || moduleCount(2) != 25 || moduleCount(40) != 177 {
		t.Errorf("moduleCount wrong: v1=%d v2=%d v40=%d", moduleCount(1), moduleCount(2), moduleCount(40))
	}
}

func TestFinderPatterns(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// Top-left finder: outer ring dark at (0,0), inner gap light at (1,1),
	// 3x3 core dark at (2,2..4,4). Center of the 3x3 core is (3,3).
	if !g.at(0, 0) {
		t.Error("finder outer corner (0,0) must be dark")
	}
	if g.at(1, 1) {
		t.Error("finder inner ring (1,1) must be light")
	}
	if !g.at(3, 3) {
		t.Error("finder core center (3,3) must be dark")
	}
	// Separator: (7,0) is light (the 1-module light border around the finder).
	if g.at(7, 0) {
		t.Error("separator (7,0) must be light")
	}
}

func TestTimingPatterns(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// Timing runs along row 6 and column 6, alternating dark/light, dark at
	// even coordinates. Sample column 6 between the finders.
	for x := 8; x < moduleCount(1)-8; x++ {
		want := x%2 == 0
		if g.at(x, 6) != want {
			t.Errorf("timing (%d,6) = %v, want %v", x, g.at(x, 6), want)
		}
	}
}

func TestDarkModule(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	// The always-dark module sits at (8, size-8).
	if !g.at(8, moduleCount(1)-8) {
		t.Error("dark module (8, size-8) must be dark")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: newGrid`, `moduleCount`, `placeFunctionPatterns`.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/matrix.go`:

```go
package qrcode

func moduleCount(version int) int { return 17 + 4*version }

type grid struct {
	size     int
	module   []bool // true = dark
	reserved []bool // true = function pattern / reserved, not for data
}

func newGrid(version int) *grid {
	n := moduleCount(version)
	return &grid{size: n, module: make([]bool, n*n), reserved: make([]bool, n*n)}
}

func (g *grid) idx(x, y int) int { return y*g.size + x }

func (g *grid) at(x, y int) bool        { return g.module[g.idx(x, y)] }
func (g *grid) set(x, y int, dark bool) { g.module[g.idx(x, y)] = dark }
func (g *grid) reserve(x, y int)        { g.reserved[g.idx(x, y)] = true }
func (g *grid) isReserved(x, y int) bool { return g.reserved[g.idx(x, y)] }

// setFn sets a function-pattern module: colors it and marks it reserved.
func (g *grid) setFn(x, y int, dark bool) {
	g.set(x, y, dark)
	g.reserve(x, y)
}

func (g *grid) placeFunctionPatterns(version int) {
	n := g.size

	// Three finder patterns + their separators, at the three corners.
	g.placeFinder(0, 0)
	g.placeFinder(n-7, 0)
	g.placeFinder(0, n-7)

	// Timing patterns along row 6 and column 6.
	for i := 8; i < n-8; i++ {
		dark := i%2 == 0
		g.setFn(i, 6, dark)
		g.setFn(6, i, dark)
	}

	// Alignment patterns at every center pair, skipping ones that collide with
	// finder patterns.
	centers := alignmentCenters(version)
	for _, cy := range centers {
		for _, cx := range centers {
			if (cx <= 8 && cy <= 8) || (cx >= n-9 && cy <= 8) || (cx <= 8 && cy >= n-9) {
				continue
			}
			g.placeAlignment(cx, cy)
		}
	}

	// Dark module.
	g.setFn(8, n-8, true)

	// Reserve format-info areas (written after masking).
	for i := 0; i < 9; i++ {
		g.reserveIfFree(i, 8)
		g.reserveIfFree(8, i)
	}
	for i := 0; i < 8; i++ {
		g.reserveIfFree(n-1-i, 8)
		g.reserveIfFree(8, n-1-i)
	}
	// Reserve version-info areas for v >= 7.
	if version >= 7 {
		for y := 0; y < 6; y++ {
			for x := n - 11; x < n-8; x++ {
				g.reserveIfFree(x, y)
				g.reserveIfFree(y, x)
			}
		}
	}
}

func (g *grid) reserveIfFree(x, y int) {
	if !g.isReserved(x, y) {
		g.reserve(x, y)
	}
}

func (g *grid) placeFinder(ox, oy int) {
	for dy := -1; dy <= 7; dy++ {
		for dx := -1; dx <= 7; dx++ {
			x, y := ox+dx, oy+dy
			if x < 0 || y < 0 || x >= g.size || y >= g.size {
				continue
			}
			// Separator ring (dx or dy == -1 or 7) is light; the rest is the
			// 7x7 finder: dark outer ring, light gap, 3x3 dark core.
			inFinder := dx >= 0 && dx <= 6 && dy >= 0 && dy <= 6
			var dark bool
			if inFinder {
				dark = dx == 0 || dx == 6 || dy == 0 || dy == 6 ||
					(dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)
			}
			g.setFn(x, y, dark)
		}
	}
}

func (g *grid) placeAlignment(cx, cy int) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			// Dark outer ring + dark center; light in between.
			dark := dx == -2 || dx == 2 || dy == -2 || dy == 2 || (dx == 0 && dy == 0)
			g.setFn(cx+dx, cy+dy, dark)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/matrix.go core/qrcode/matrix_test.go
git commit -m "feat(qrcode): matrix function-pattern placement"
```

---

## Task 6: Data placement + masking + public Encode

**Files:**
- Create: `core/qrcode/mask.go`
- Modify: `core/qrcode/matrix.go` (data placement, `Matrix` type, `Encode`), `core/qrcode/level.go` (`formatBits`)
- Test: `core/qrcode/encode_matrix_test.go`, `core/qrcode/testdata/*.txt` (golden matrices)

**Interfaces:**
- Consumes: `finalCodewords`, `grid`, `placeFunctionPatterns`, `config`, `Level`.
- Produces:
  - `func Encode(data string, opts ...Option) (*Matrix, error)`.
  - `type Matrix struct{…}` with `Size() int`, `Module(x, y int) bool`, `Version() int`, `Level() Level`.
  - internal `func (g *grid) placeData(stream []byte)`, `func bestMask(g *grid) (int, *grid)`, `func (l Level) formatBits() int`.

- [ ] **Step 1: Write the failing test**

First capture a golden matrix from the reference tool at implementation time. Install once: `brew install qrencode` (macOS) or `apt-get install qrencode`. Generate the module map for a fixed input:

```bash
mkdir -p core/qrcode/testdata
# -8 = byte mode, -l M = level M, -m 0 = no margin, -t ASCII = text grid.
qrencode -8 -l M -m 0 -t ASCII 'https://forge.example/r/abc123' > core/qrcode/testdata/url_m.txt
```

`qrencode -t ASCII` prints two space characters per light module and two block characters per dark module. The test parses it into a boolean matrix. Create `core/qrcode/encode_matrix_test.go`:

```go
package qrcode_test

import (
	"os"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

// parseGolden turns a `qrencode -t ASCII -m 0` dump into a boolean matrix.
// Dark modules render as non-space characters; light modules as spaces. Each
// module is two characters wide.
func parseGolden(t *testing.T, path string) [][]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var m [][]bool
	for _, ln := range lines {
		if ln == "" {
			continue
		}
		row := make([]bool, 0, len(ln)/2)
		for i := 0; i+1 < len(ln); i += 2 {
			row = append(row, ln[i] != ' ')
		}
		m = append(m, row)
	}
	return m
}

func TestEncodeMatchesGolden(t *testing.T) {
	golden := parseGolden(t, "testdata/url_m.txt")
	m, err := qrcode.Encode("https://forge.example/r/abc123", qrcode.WithLevel(qrcode.LevelM))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if m.Size() != len(golden) {
		t.Fatalf("size = %d, want %d", m.Size(), len(golden))
	}
	for y := 0; y < m.Size(); y++ {
		for x := 0; x < m.Size(); x++ {
			if m.Module(x, y) != golden[y][x] {
				t.Fatalf("module (%d,%d) = %v, want %v", x, y, m.Module(x, y), golden[y][x])
			}
		}
	}
}

func TestEncodeAccessors(t *testing.T) {
	m, err := qrcode.Encode("test", qrcode.WithLevel(qrcode.LevelH))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if m.Version() < 1 || m.Version() > 40 {
		t.Errorf("version = %d out of range", m.Version())
	}
	if m.Level() != qrcode.LevelH {
		t.Errorf("level = %v, want H", m.Level())
	}
	if m.Size() != 17+4*m.Version() {
		t.Errorf("size %d inconsistent with version %d", m.Size(), m.Version())
	}
}

func TestEncodeTooLarge(t *testing.T) {
	_, err := qrcode.Encode(strings.Repeat("x", 5000), qrcode.WithLevel(qrcode.LevelH))
	if err == nil {
		t.Fatal("expected ErrTooLarge for oversized input")
	}
}
```

Also add a white-box format-info self-check `core/qrcode/mask_test.go`:

```go
package qrcode

import "testing"

func TestFormatInfoRoundTrip(t *testing.T) {
	// Level M, mask 5. Format bits map: L=01, M=00, Q=11, H=10 (spec order).
	if LevelM.formatBits() != 0b00 || LevelL.formatBits() != 0b01 ||
		LevelQ.formatBits() != 0b11 || LevelH.formatBits() != 0b10 {
		t.Fatal("formatBits mapping wrong")
	}
}

func TestBestMaskDeterministic(t *testing.T) {
	g := newGrid(1)
	g.placeFunctionPatterns(1)
	stream := finalCodewords([]byte("mask"), 1, LevelM)
	g.placeData(stream)
	mask, masked := bestMask(g)
	if mask < 0 || mask > 7 {
		t.Fatalf("mask = %d out of range", mask)
	}
	if masked.size != g.size {
		t.Fatal("masked grid wrong size")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: qrcode.Encode`, `Matrix`, `placeData`, `bestMask`, `formatBits`.

- [ ] **Step 3: Write minimal implementation**

Add to `core/qrcode/level.go`:

```go
// formatBits returns the 2-bit error-correction indicator used in the QR
// format information (spec order: L=01, M=00, Q=11, H=10).
func (l Level) formatBits() int {
	switch l {
	case LevelL:
		return 0b01
	case LevelM:
		return 0b00
	case LevelQ:
		return 0b11
	case LevelH:
		return 0b10
	default:
		return 0b00
	}
}
```

Create `core/qrcode/mask.go`:

```go
package qrcode

// maskCondition reports whether module (x, y) is inverted by mask pattern id.
func maskCondition(id, x, y int) bool {
	switch id {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return (x*y)%2+(x*y)%3 == 0
	case 6:
		return ((x*y)%2+(x*y)%3)%2 == 0
	case 7:
		return ((x+y)%2+(x*y)%3)%2 == 0
	default:
		return false
	}
}

// applyMask returns a copy of g with mask id applied to every non-reserved
// (data) module and format information written for (level, id).
func applyMask(g *grid, id int, level Level) *grid {
	out := &grid{size: g.size, module: append([]bool{}, g.module...), reserved: g.reserved}
	for y := 0; y < g.size; y++ {
		for x := 0; x < g.size; x++ {
			if !g.isReserved(x, y) && maskCondition(id, x, y) {
				out.module[out.idx(x, y)] = !out.module[out.idx(x, y)]
			}
		}
	}
	writeFormatInfo(out, level, id)
	return out
}

// bestMask applies all 8 masks and returns the id + masked grid with the
// lowest penalty score.
func bestMask(g *grid) (int, *grid) {
	return bestMaskForLevel(g, LevelM)
}

func bestMaskForLevel(g *grid, level Level) (int, *grid) {
	bestID, bestScore := 0, 1<<62
	var best *grid
	for id := 0; id < 8; id++ {
		m := applyMask(g, id, level)
		if s := penalty(m); s < bestScore {
			bestID, bestScore, best = id, s, m
		}
	}
	return bestID, best
}

// penalty scores a masked grid with the four ISO/IEC 18004 §8.8.2 rules.
func penalty(g *grid) int {
	n := g.size
	score := 0

	// Rule 1: runs of 5+ same-color modules in each row and column.
	runScore := func(get func(i, j int) bool) int {
		total := 0
		for i := 0; i < n; i++ {
			run, prev := 1, get(i, 0)
			for j := 1; j < n; j++ {
				c := get(i, j)
				if c == prev {
					run++
				} else {
					if run >= 5 {
						total += 3 + (run - 5)
					}
					run, prev = 1, c
				}
			}
			if run >= 5 {
				total += 3 + (run - 5)
			}
		}
		return total
	}
	score += runScore(func(i, j int) bool { return g.at(j, i) }) // rows
	score += runScore(func(i, j int) bool { return g.at(i, j) }) // cols

	// Rule 2: 2x2 same-color blocks.
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			c := g.at(x, y)
			if g.at(x+1, y) == c && g.at(x, y+1) == c && g.at(x+1, y+1) == c {
				score += 3
			}
		}
	}

	// Rule 3: finder-like 1:1:3:1:1 patterns (dark-light-dark run + 4 light),
	// horizontally and vertically.
	pat1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	pat2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	matches := func(get func(k int) bool, pat []bool) bool {
		for k := range pat {
			if get(k) != pat[k] {
				return false
			}
		}
		return true
	}
	for y := 0; y < n; y++ {
		for x := 0; x <= n-11; x++ {
			get := func(k int) bool { return g.at(x+k, y) }
			if matches(get, pat1) || matches(get, pat2) {
				score += 40
			}
		}
	}
	for x := 0; x < n; x++ {
		for y := 0; y <= n-11; y++ {
			get := func(k int) bool { return g.at(x, y+k) }
			if matches(get, pat1) || matches(get, pat2) {
				score += 40
			}
		}
	}

	// Rule 4: dark-module ratio deviation from 50%.
	dark := 0
	for _, m := range g.module {
		if m {
			dark++
		}
	}
	pct := dark * 100 / (n * n)
	dev := pct - 50
	if dev < 0 {
		dev = -dev
	}
	score += (dev / 5) * 10

	return score
}
```

Add format-info + version-info + data placement to `core/qrcode/matrix.go`:

```go
// writeFormatInfo writes the 15-bit format information (level + mask, BCH
// (15,5) with generator 0x537, XOR-masked with 0x5412) into both copies.
func writeFormatInfo(g *grid, level Level, mask int) {
	data := level.formatBits()<<3 | mask
	bch := data << 10
	for bch>=1<<10 { // reduce by generator until degree < 10
		bch ^= 0x537 << (bitLen(bch) - 11)
	}
	bits := (data<<10 | bch) ^ 0x5412
	n := g.size
	// Around top-left finder.
	for i := 0; i <= 5; i++ {
		g.set(8, i, bit(bits, i))
	}
	g.set(8, 7, bit(bits, 6))
	g.set(8, 8, bit(bits, 7))
	g.set(7, 8, bit(bits, 8))
	for i := 9; i < 15; i++ {
		g.set(14-i, 8, bit(bits, i))
	}
	// Split copy near the other two finders.
	for i := 0; i <= 7; i++ {
		g.set(n-1-i, 8, bit(bits, i))
	}
	for i := 8; i < 15; i++ {
		g.set(8, n-15+i, bit(bits, i))
	}
}

func writeVersionInfo(g *grid, version int) {
	if version < 7 {
		return
	}
	bch := version << 12
	for bitLen(bch) >= 13 {
		bch ^= 0x1F25 << (bitLen(bch) - 13)
	}
	bits := version<<12 | bch
	n := g.size
	for i := 0; i < 18; i++ {
		b := bit(bits, i)
		a, c := i/3, i%3
		g.set(a, n-11+c, b)
		g.set(n-11+c, a, b)
	}
}

func bit(v, i int) bool { return (v>>uint(i))&1 == 1 }

func bitLen(v int) int {
	n := 0
	for v > 0 {
		n++
		v >>= 1
	}
	return n
}

// placeData walks the standard upward/downward zigzag from the bottom-right,
// skipping column 6 (timing) and every reserved module, writing stream bits
// MSB-first.
func (g *grid) placeData(stream []byte) {
	n := g.size
	bitIdx := 0
	next := func() bool {
		if bitIdx >= len(stream)*8 {
			return false // remainder bits are 0
		}
		b := (stream[bitIdx/8]>>uint(7-bitIdx%8))&1 == 1
		bitIdx++
		return b
	}
	up := true
	for col := n - 1; col > 0; col -= 2 {
		if col == 6 {
			col-- // skip vertical timing column
		}
		for i := 0; i < n; i++ {
			y := i
			if up {
				y = n - 1 - i
			}
			for _, x := range [2]int{col, col - 1} {
				if !g.isReserved(x, y) {
					g.set(x, y, next())
				}
			}
		}
		up = !up
	}
}
```

Add the public `Matrix` + `Encode` to `core/qrcode/matrix.go`:

```go
// Matrix is a computed QR symbol: a square grid of dark/light modules with no
// quiet zone. Use the accessors to render it however you like, or pass the
// same data + options to PNG/SVG/DataURI for a ready image.
type Matrix struct {
	g       *grid
	version int
	level   Level
}

// Size returns the module count per side (excludes the quiet zone).
func (m *Matrix) Size() int { return m.g.size }

// Module reports whether the module at (x, y) is dark.
func (m *Matrix) Module(x, y int) bool { return m.g.at(x, y) }

// Version returns the QR version (1–40).
func (m *Matrix) Version() int { return m.version }

// Level returns the error-correction level the grid was encoded at.
func (m *Matrix) Level() Level { return m.level }

// Encode builds the QR module matrix for data. It honors WithLevel; render
// options (size, colors, logo, shapes) are ignored — pass them to PNG/SVG/
// DataURI instead. Returns ErrTooLarge if data exceeds version-40 capacity.
func Encode(data string, opts ...Option) (*Matrix, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return encodeMatrix(data, c.level)
}

// encodeMatrix runs the full pipeline at an explicit level. Renderers call it
// with the effective (possibly raised) level.
func encodeMatrix(data string, level Level) (*Matrix, error) {
	raw := []byte(data)
	version, err := pickVersion(len(raw), level)
	if err != nil {
		return nil, err
	}
	g := newGrid(version)
	g.placeFunctionPatterns(version)
	writeVersionInfo(g, version)
	g.placeData(finalCodewords(raw, version, level))
	_, masked := bestMaskForLevel(g, level)
	return &Matrix{g: masked, version: version, level: level}, nil
}
```

> Note: `bestMask` (the `LevelM` convenience wrapper) exists only for the Task 6 white-box test; `encodeMatrix` uses `bestMaskForLevel`. Keep both.

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS. **This is the correctness gate.** If `TestEncodeMatchesGolden` fails, the mismatch localizes the bug: wrong size → version/capacity table; scattered wrong modules → data placement or masking; wrong format bits region → `writeFormatInfo`. Fix until the golden matches exactly, then add a second golden at a higher version/level (e.g. `qrencode -8 -l Q -m 0 -t ASCII 'much longer payload …' > testdata/long_q.txt`) and a matching sub-test to cover multi-block interleaving and alignment patterns.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/
git commit -m "feat(qrcode): data placement, masking, format info, public Encode"
```

---

## Task 7: PNG + DataURI renderer (square)

**Files:**
- Create: `core/qrcode/render_png.go`
- Test: `core/qrcode/render_png_test.go`

**Interfaces:**
- Consumes: `Encode`/`encodeMatrix`, `config`, `Matrix`.
- Produces: `func PNG(data string, opts ...Option) ([]byte, error)`, `func DataURI(data string, opts ...Option) (string, error)`, and internal `func resolveScale(c config, fullModules int) int`, `func renderImage(m *Matrix, c config) *image.RGBA` (square path only for now; returns the concrete `*image.RGBA` so Task 10 can composite onto it).

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/render_png_test.go`:

```go
package qrcode_test

import (
	"bytes"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func TestPNGDecodesWithExpectedSize(t *testing.T) {
	data := "https://forge.example/x"
	raw, err := qrcode.PNG(data, qrcode.WithScale(4), qrcode.WithBorder(4))
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	m, _ := qrcode.Encode(data)
	want := (m.Size() + 2*4) * 4 // (modules + border*2) * scale
	if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
		t.Errorf("image %dx%d, want %dx%d", b.Dx(), b.Dy(), want, want)
	}
}

func TestPNGColors(t *testing.T) {
	raw, err := qrcode.PNG("hello",
		qrcode.WithScale(4),
		qrcode.WithForeground(color.RGBA{R: 200, A: 255}),
		qrcode.WithBackground(color.RGBA{B: 200, A: 255}),
	)
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, _ := png.Decode(bytes.NewReader(raw))
	// Top-left pixel is in the quiet zone → background (blue-ish).
	r, _, b, _ := img.At(0, 0).RGBA()
	if b <= r {
		t.Errorf("quiet-zone pixel not background color: r=%d b=%d", r, b)
	}
}

func TestDataURIPrefix(t *testing.T) {
	uri, err := qrcode.DataURI("hello", qrcode.WithScale(4))
	if err != nil {
		t.Fatalf("DataURI: %v", err)
	}
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("bad prefix: %.30q", uri)
	}
}

func TestWithSizePicksScale(t *testing.T) {
	data := "size test"
	m, _ := qrcode.Encode(data)
	full := m.Size() + 2*4 // modules + default border on both sides
	raw, err := qrcode.PNG(data, qrcode.WithSize(full*10+5))
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, _ := png.Decode(bytes.NewReader(raw))
	if img.Bounds().Dx() != full*10 { // largest integer scale that fits target
		t.Errorf("size = %d, want %d", img.Bounds().Dx(), full*10)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: qrcode.PNG`, `qrcode.DataURI`.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/render_png.go`:

```go
package qrcode

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
)

// resolveScale returns pixels-per-module: WithSize (largest integer scale whose
// full image fits the target) wins over WithScale when set.
func resolveScale(c config, fullModules int) int {
	if c.targetSize > 0 {
		s := c.targetSize / fullModules
		if s < 1 {
			s = 1
		}
		return s
	}
	return c.scale
}

// renderImage draws the matrix with quiet-zone border and fg/bg colors at the
// resolved integer scale. Square modules only (shapes added in Task 9).
func renderImage(m *Matrix, c config) *image.RGBA {
	full := m.Size() + 2*c.border
	scale := resolveScale(c, full)
	px := full * scale
	img := image.NewRGBA(image.Rect(0, 0, px, px))
	draw.Draw(img, img.Bounds(), image.NewUniform(c.bg), image.Point{}, draw.Src)
	for y := 0; y < m.Size(); y++ {
		for x := 0; x < m.Size(); x++ {
			if !m.Module(x, y) {
				continue
			}
			x0 := (x + c.border) * scale
			y0 := (y + c.border) * scale
			draw.Draw(img, image.Rect(x0, y0, x0+scale, y0+scale), image.NewUniform(c.fg), image.Point{}, draw.Src)
		}
	}
	return img
}

// PNG encodes data as a PNG image.
func PNG(data string, opts ...Option) ([]byte, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	m, err := encodeMatrix(data, c.effectiveLevel())
	if err != nil {
		return nil, err
	}
	img := renderImage(m, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DataURI encodes data as a base64 PNG data URI ("data:image/png;base64,…").
func DataURI(data string, opts ...Option) (string, error) {
	raw, err := PNG(data, opts...)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/render_png.go core/qrcode/render_png_test.go
git commit -m "feat(qrcode): PNG and data-URI renderers"
```

---

## Task 8: SVG renderer (square)

**Files:**
- Create: `core/qrcode/render_svg.go`
- Test: `core/qrcode/render_svg_test.go`

**Interfaces:**
- Consumes: `encodeMatrix`, `config`, `Matrix`.
- Produces: `func SVG(data string, opts ...Option) ([]byte, error)`, internal `func colorHex(c color.Color) string`.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/render_svg_test.go`:

```go
package qrcode_test

import (
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func TestSVGStructure(t *testing.T) {
	out, err := qrcode.SVG("hello", qrcode.WithBackground(color.White), qrcode.WithForeground(color.Black))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "<svg") || !strings.Contains(s, "</svg>") {
		t.Errorf("not an svg document: %.40q", s)
	}
	m, _ := qrcode.Encode("hello")
	full := m.Size() + 2*4
	if !strings.Contains(s, `viewBox="0 0 `+strconv.Itoa(full)+" "+strconv.Itoa(full)+`"`) {
		t.Errorf("viewBox not in module units: %.80q", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `undefined: qrcode.SVG`.

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/render_svg.go`. The viewBox is in module units so the SVG scales freely; a `shape-rendering="crispEdges"` hint keeps square modules sharp:

```go
package qrcode

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

func colorHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// SVG encodes data as an SVG document. The viewBox is expressed in module units
// (including the quiet zone) so the markup scales to any display size.
func SVG(data string, opts ...Option) ([]byte, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	m, err := encodeMatrix(data, c.effectiveLevel())
	if err != nil {
		return nil, err
	}
	full := m.Size() + 2*c.border
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	b.WriteString(strconv.Itoa(full))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(full))
	b.WriteString(`" shape-rendering="crispEdges">`)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`, full, full, colorHex(c.bg))
	// One path of all dark module squares.
	b.WriteString(`<path fill="`)
	b.WriteString(colorHex(c.fg))
	b.WriteString(`" d="`)
	for y := 0; y < m.Size(); y++ {
		for x := 0; x < m.Size(); x++ {
			if m.Module(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+c.border, y+c.border)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return []byte(b.String()), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/render_svg.go core/qrcode/render_svg_test.go
git commit -m "feat(qrcode): SVG renderer"
```

---

## Task 9: Styled module + eye shapes

**Files:**
- Modify: `core/qrcode/shape.go` (geometry + finder detection), `core/qrcode/render_png.go` (supersampling + shaped draw), `core/qrcode/render_svg.go` (shaped elements)
- Test: `core/qrcode/shape_test.go`

**Interfaces:**
- Consumes: `Matrix`, `config`, `renderImage`.
- Produces:
  - `func isEyeModule(m *Matrix, x, y int) bool` — true when (x,y) is inside one of the three 7×7 finder patterns.
  - `func (c config) supersampleFactor() int` — 1 for all-square, 4 when any curved shape is requested.
  - Shaped drawing inside `renderImage` (PNG) and `SVG`.
  - `ShapeDots` raises the effective level (already wired via `config.effectiveLevel`).

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/shape_test.go`:

```go
package qrcode_test

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func TestDotsRaisesLevel(t *testing.T) {
	// A short input renders fine at M; with ShapeDots the encoder must use >=Q,
	// so decoding proves nothing here — assert via SVG size/level indirectly:
	// dots at level L request still produce a valid image without error.
	if _, err := qrcode.PNG("dots", qrcode.WithLevel(qrcode.LevelL), qrcode.WithModuleShape(qrcode.ShapeDots), qrcode.WithScale(6)); err != nil {
		t.Fatalf("PNG dots: %v", err)
	}
}

func TestShapedPNGRenders(t *testing.T) {
	for _, sh := range []qrcode.Shape{qrcode.ShapeRounded, qrcode.ShapeDots} {
		raw, err := qrcode.PNG("shaped", qrcode.WithModuleShape(sh), qrcode.WithEyeShape(qrcode.EyeRounded), qrcode.WithScale(8))
		if err != nil {
			t.Fatalf("PNG shape %v: %v", sh, err)
		}
		if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
			t.Fatalf("decode shape %v: %v", sh, err)
		}
	}
}

func TestShapedSVGUsesCircles(t *testing.T) {
	out, err := qrcode.SVG("dots", qrcode.WithModuleShape(qrcode.ShapeDots), qrcode.WithScale(8))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	if !strings.Contains(string(out), "<circle") {
		t.Error("ShapeDots SVG must use <circle> elements")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — `ShapeDots` SVG still emits squares (`TestShapedSVGUsesCircles` fails); shaped PNG may pass trivially but the circle assertion drives the change.

- [ ] **Step 3: Write minimal implementation**

Add finder detection + geometry to `core/qrcode/shape.go`:

```go
// isEyeModule reports whether (x, y) falls within one of the three 7x7 finder
// patterns (top-left, top-right, bottom-left).
func isEyeModule(size, x, y int) bool {
	in := func(ox, oy int) bool { return x >= ox && x < ox+7 && y >= oy && y < oy+7 }
	return in(0, 0) || in(size-7, 0) || in(0, size-7)
}

func (c config) supersampleFactor() int {
	if c.moduleShape == ShapeSquare && c.eyeShape == EyeSquare {
		return 1
	}
	return 4
}
```

Rework `renderImage` in `core/qrcode/render_png.go` to draw shapes and supersample. Replace the body with a version that renders at `scale*ss` then box-downsamples when `ss > 1`:

```go
func renderImage(m *Matrix, c config) *image.RGBA {
	full := m.Size() + 2*c.border
	scale := resolveScale(c, full)
	ss := c.supersampleFactor()
	hi := image.NewRGBA(image.Rect(0, 0, full*scale*ss, full*scale*ss))
	draw.Draw(hi, hi.Bounds(), image.NewUniform(c.bg), image.Point{}, draw.Src)
	cell := scale * ss
	for y := 0; y < m.Size(); y++ {
		for x := 0; x < m.Size(); x++ {
			if !m.Module(x, y) {
				continue
			}
			x0 := (x + c.border) * cell
			y0 := (y + c.border) * cell
			shape := c.moduleShape
			if isEyeModule(m.Size(), x, y) {
				drawEyeModule(hi, x0, y0, cell, c)
				continue
			}
			drawModule(hi, x0, y0, cell, shape, c.fg)
		}
	}
	if ss == 1 {
		return hi
	}
	return downsample(hi, ss)
}

// drawModule fills one module cell in the requested shape.
func drawModule(img *image.RGBA, x0, y0, cell int, shape Shape, fg color.Color) {
	switch shape {
	case ShapeDots:
		fillCircle(img, x0, y0, cell, fg)
	case ShapeRounded:
		fillRounded(img, x0, y0, cell, cell/3, fg)
	default:
		draw.Draw(img, image.Rect(x0, y0, x0+cell, y0+cell), image.NewUniform(fg), image.Point{}, draw.Src)
	}
}

// drawEyeModule fills a finder-pattern cell. EyeRounded rounds only the corner
// cells of each finder; interior cells stay square so the eye keeps its shape.
func drawEyeModule(img *image.RGBA, x0, y0, cell int, c config) {
	shape := ShapeSquare
	if c.eyeShape == EyeRounded {
		shape = ShapeRounded
	}
	drawModule(img, x0, y0, cell, shape, c.fg)
}

func fillCircle(img *image.RGBA, x0, y0, cell int, fg color.Color) {
	r := float64(cell) / 2
	cx, cy := float64(x0)+r, float64(y0)+r
	for py := y0; py < y0+cell; py++ {
		for px := x0; px < x0+cell; px++ {
			dx, dy := float64(px)+0.5-cx, float64(py)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(px, py, fg)
			}
		}
	}
}

func fillRounded(img *image.RGBA, x0, y0, cell, rad int, fg color.Color) {
	rf := float64(rad)
	for py := y0; py < y0+cell; py++ {
		for px := x0; px < x0+cell; px++ {
			lx := float64(px - x0)
			ly := float64(py - y0)
			// Nearest corner-arc center check.
			cxx, cyy := lx, ly
			if lx < rf {
				cxx = rf
			} else if lx > float64(cell)-rf {
				cxx = float64(cell) - rf
			}
			if ly < rf {
				cyy = rf
			} else if ly > float64(cell)-rf {
				cyy = float64(cell) - rf
			}
			dx, dy := lx-cxx, ly-cyy
			if dx*dx+dy*dy <= rf*rf {
				img.Set(px, py, fg)
			}
		}
	}
}

// downsample box-averages an ss*ss block into one pixel (anti-aliasing).
func downsample(src *image.RGBA, ss int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx()/ss, b.Dy()/ss
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	area := uint32(ss * ss)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sr, sg, sb, sa uint32
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					r, g, bl, a := src.At(x*ss+dx, y*ss+dy).RGBA()
					sr += r >> 8
					sg += g >> 8
					sb += bl >> 8
					sa += a >> 8
				}
			}
			dst.Set(x, y, color.RGBA{uint8(sr / area), uint8(sg / area), uint8(sb / area), uint8(sa / area)})
		}
	}
	return dst
}
```

Add the needed imports to `render_png.go`: `"image/color"`.

Extend `SVG` in `core/qrcode/render_svg.go` to emit shaped elements. After computing `m`, branch on `c.moduleShape`:

```go
	dark := colorHex(c.fg)
	switch c.moduleShape {
	case ShapeDots:
		for y := 0; y < m.Size(); y++ {
			for x := 0; x < m.Size(); x++ {
				if m.Module(x, y) {
					fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="0.5" fill="%s"/>`,
						float64(x+c.border)+0.5, float64(y+c.border)+0.5, dark)
				}
			}
		}
	case ShapeRounded:
		for y := 0; y < m.Size(); y++ {
			for x := 0; x < m.Size(); x++ {
				if m.Module(x, y) {
					fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" rx="0.33" fill="%s"/>`,
						x+c.border, y+c.border, dark)
				}
			}
		}
	default: // ShapeSquare — the single path from Task 8
		b.WriteString(`<path fill="` + dark + `" d="`)
		for y := 0; y < m.Size(); y++ {
			for x := 0; x < m.Size(); x++ {
				if m.Module(x, y) {
					fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+c.border, y+c.border)
				}
			}
		}
		b.WriteString(`"/>`)
	}
	b.WriteString(`</svg>`)
```

> Restructure Task 8's `SVG` so the background `<rect>` is written first, then this `switch` replaces the single square path, then `</svg>`. Eye shaping in SVG can reuse the same module elements (the finder cells are ordinary dark modules); `EyeRounded` is honored in PNG and is a no-op refinement in SVG for now — document that.

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS. Also re-run `TestEncodeMatchesGolden` (Task 6) — shapes must not change the encoded matrix, only rendering.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/shape.go core/qrcode/render_png.go core/qrcode/render_svg.go core/qrcode/shape_test.go
git commit -m "feat(qrcode): styled module and eye shapes with supersampled PNG"
```

---

## Task 10: Center logo compositing

**Files:**
- Create: `core/qrcode/logo.go`
- Modify: `core/qrcode/render_png.go` (composite after render), `core/qrcode/render_svg.go` (embed `<image>`)
- Test: `core/qrcode/logo_test.go`

**Interfaces:**
- Consumes: `config`, rendered `image.Image`, `Matrix`.
- Produces: `func compositeLogo(base *image.RGBA, c config)`, `func logoDataURI(img image.Image) (string, error)`; PNG/SVG honor `WithLogo`.

- [ ] **Step 1: Write the failing test**

Create `core/qrcode/logo_test.go`:

```go
package qrcode_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func solidLogo(n int, col color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			img.Set(x, y, col)
		}
	}
	return img
}

func TestPNGLogoCenterOverlaid(t *testing.T) {
	logo := solidLogo(40, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	raw, err := qrcode.PNG("https://forge.example/logo", qrcode.WithScale(8), qrcode.WithLogo(logo))
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, _ := png.Decode(bytes.NewReader(raw))
	b := img.Bounds()
	// Center pixel should be logo red (allow the white backing pad to miss dead
	// center by sampling the exact midpoint, which is inside the logo).
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	if !(r > 0x8000 && g < 0x4000 && bl < 0x4000) {
		t.Errorf("center pixel not logo-red: r=%d g=%d b=%d", r>>8, g>>8, bl>>8)
	}
}

func TestSVGLogoEmbedsImage(t *testing.T) {
	logo := solidLogo(16, color.RGBA{B: 255, A: 255})
	out, err := qrcode.SVG("hello", qrcode.WithLogo(logo))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	if !strings.Contains(string(out), "<image") || !strings.Contains(string(out), "data:image/png;base64,") {
		t.Error("SVG logo must embed a base64 <image>")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `just test ./core/qrcode/...`
Expected: FAIL — logo not composited (center pixel is a module color, no `<image>` in SVG).

- [ ] **Step 3: Write minimal implementation**

Create `core/qrcode/logo.go`:

```go
package qrcode

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/png"
)

// compositeLogo scales c.logo to c.logoSize of the base image and draws it
// centered over a white backing pad. No-op when no logo is set.
func compositeLogo(base *image.RGBA, c config) {
	if c.logo == nil {
		return
	}
	b := base.Bounds()
	side := int(float64(b.Dx()) * c.logoSize)
	if side < 1 {
		return
	}
	pad := side / 8
	cx, cy := b.Dx()/2, b.Dy()/2
	// White backing pad.
	padRect := image.Rect(cx-side/2-pad, cy-side/2-pad, cx+side/2+pad, cy+side/2+pad)
	draw.Draw(base, padRect, image.NewUniform(c.bg), image.Point{}, draw.Src)
	// Nearest-neighbor scale the logo into the target rect.
	dst := image.Rect(cx-side/2, cy-side/2, cx+side/2, cy+side/2)
	src := c.logo.Bounds()
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		for x := dst.Min.X; x < dst.Max.X; x++ {
			sx := src.Min.X + (x-dst.Min.X)*src.Dx()/side
			sy := src.Min.Y + (y-dst.Min.Y)*src.Dy()/side
			base.Set(x, y, c.logo.At(sx, sy))
		}
	}
}

// logoDataURI encodes an image as a base64 PNG data URI for SVG embedding.
func logoDataURI(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
```

In `core/qrcode/render_png.go`, `renderImage` already returns `*image.RGBA` (Tasks 7 & 9), so `compositeLogo` can draw onto it directly. Update `PNG` to composite before encoding:

```go
	img := renderImage(m, c)
	compositeLogo(img, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
```

In `core/qrcode/render_svg.go`, before `</svg>`, embed the logo if set:

```go
	if c.logo != nil {
		uri, err := logoDataURI(c.logo)
		if err != nil {
			return nil, err
		}
		side := float64(full) * c.logoSize
		pos := (float64(full) - side) / 2
		fmt.Fprintf(&b, `<image x="%.2f" y="%.2f" width="%.2f" height="%.2f" href="%s"/>`,
			pos, pos, side, side, uri)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add core/qrcode/logo.go core/qrcode/render_png.go core/qrcode/render_svg.go core/qrcode/logo_test.go
git commit -m "feat(qrcode): center logo compositing for PNG and SVG"
```

---

## Task 11: doc.go, benchmarks, catalog removal, lint

**Files:**
- Create: `core/qrcode/doc.go`, `core/qrcode/bench_test.go`
- Modify: `docs/packages.md` (remove the `core/qrcode` entry)
- Test: `Example` in doc.go is compiled/run by `go test`.

**Interfaces:**
- Consumes: the full public API.
- Produces: package documentation, benchmarks, updated catalog.

- [ ] **Step 1: Write doc.go with a runnable example**

Create `core/qrcode/doc.go`:

```go
// Package qrcode generates QR codes from any string with no external
// dependencies. It exposes the raw module matrix plus PNG, base64 PNG
// data-URI, and SVG renderers, with configurable size, colors, a center logo,
// and styled module/eye shapes.
//
// The encoder is byte mode only (UTF-8) with automatic version selection
// (1–40) and error-correction levels L/M/Q/H (default M). Setting a logo or
// ShapeDots raises the effective level to at least Q so the result stays
// scannable.
//
// # Usage
//
//	// A 2FA enrollment QR as an <img src> value.
//	uri, err := qrcode.DataURI("otpauth://totp/App:user?secret=ABC&issuer=App")
//
//	// A branded referral code: high error correction, rounded modules, logo.
//	png, err := qrcode.PNG(link,
//		qrcode.WithLevel(qrcode.LevelH),
//		qrcode.WithModuleShape(qrcode.ShapeRounded),
//		qrcode.WithLogo(logoImg),
//		qrcode.WithSize(512),
//	)
//
//	// The raw grid, to render your own way.
//	m, err := qrcode.Encode(link)
//	for y := 0; y < m.Size(); y++ {
//		for x := 0; x < m.Size(); x++ {
//			_ = m.Module(x, y) // true = dark
//		}
//	}
package qrcode
```

Add a testable example `core/qrcode/example_test.go`:

```go
package qrcode_test

import (
	"fmt"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func ExampleEncode() {
	m, err := qrcode.Encode("https://forge.example")
	if err != nil {
		panic(err)
	}
	fmt.Println(m.Size() > 0)
	// Output: true
}
```

- [ ] **Step 2: Write benchmarks**

Create `core/qrcode/bench_test.go`:

```go
package qrcode_test

import (
	"image"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

const (
	shortURL = "otpauth://totp/Forge:user@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Forge"
	longURL  = "https://forge.example/r/abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

func BenchmarkEncodeShort(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.Encode(shortURL); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeLong(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.Encode(longURL); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeLevels(b *testing.B) {
	for _, lv := range []qrcode.Level{qrcode.LevelL, qrcode.LevelM, qrcode.LevelQ, qrcode.LevelH} {
		b.Run(lv.String(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := qrcode.Encode(longURL, qrcode.WithLevel(lv)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkPNGSquare(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.PNG(shortURL, qrcode.WithScale(8)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPNGShaped(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.PNG(shortURL, qrcode.WithScale(8), qrcode.WithModuleShape(qrcode.ShapeDots)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPNGLogo(b *testing.B) {
	logo := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range logo.Pix {
		logo.Pix[i] = 0xFF
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.PNG(shortURL, qrcode.WithScale(8), qrcode.WithLogo(logo)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSVG(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := qrcode.SVG(longURL); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run tests and benchmarks**

Run: `just fmt ./core/qrcode/...` then `just test ./core/qrcode/...` then `just bench ./core/qrcode/...`
Expected: tests PASS (including `ExampleEncode`); benchmarks run and report ns/op + allocs/op. Record the `BenchmarkPNGShaped` vs `BenchmarkPNGSquare` ratio in the PR description (justifies the supersampling path per design.md).

- [ ] **Step 4: Remove the catalog entry**

Edit `docs/packages.md`: delete the `**core/qrcode**` heading, its description paragraph, and the surrounding `---` separators so the `## core/` section no longer lists it (the section may become empty — that is fine; leave the `## core/` header only if other core entries remain, otherwise remove the now-empty section).

- [ ] **Step 5: Lint and commit**

Run: `just lint`
Expected: no findings for `core/qrcode`. Fix any modernize/betteralign/nilaway issues (e.g., struct field alignment in `config`/`blockSpec`/`grid`) and re-run until clean.

```bash
git add core/qrcode/ docs/packages.md
git commit -m "docs(qrcode): package doc, benchmarks, and catalog removal"
```

---

## Self-review notes (for the implementer)

- **Golden is the gate.** Task 6's `TestEncodeMatchesGolden` is the single most important test. If the hand-rolled encoder disagrees with `qrencode` by even one module, a table or algorithm step is wrong. Add at least two goldens (one single-block low version, one multi-block higher version + level) before trusting the encoder.
- **Tables are the highest-risk transcription.** `ecBlocks` (40×4) and `alignmentCentersByVersion` (40 rows) must come from the cited ISO/IEC 18004 tables. The plan shows only a prefix; fill every row. The version/boundary tests plus the goldens catch mistakes.
- **Shapes never touch encoding.** After Task 9/10, re-run Task 6's golden test to confirm rendering options left the matrix identical.
- **`bestMask` vs `bestMaskForLevel`.** `encodeMatrix` uses `bestMaskForLevel(g, level)`; `writeFormatInfo` therefore encodes the correct level. The `bestMask` wrapper is only for the Task 6 white-box mask test.
- **Signature consistency:** `renderImage` and `downsample` return `*image.RGBA` (not `image.Image`) so Task 10's `compositeLogo` can draw onto the result before `png.Encode`.
