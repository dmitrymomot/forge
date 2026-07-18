package qrcode_test

import (
	"bytes"
	"image/png"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func TestDotsRaisesLevel(t *testing.T) {
	// ShapeDots forces effectiveLevel to raise the requested LevelL to >=Q, so
	// decoding the output wouldn't confirm LevelL took effect. This just checks
	// the combination still produces a valid image without error.
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

// countForeground decodes a PNG and returns the number of pixels that exactly
// match the default foreground color (opaque black). Curved shapes carve
// foreground area out of each dark cell, so this count drops when the shape
// dispatch is actually applied — a square-fallback regression makes the counts
// equal and fails TestShapedPNGDispatchAltersRaster.
func countForeground(t *testing.T, raw []byte) int {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			// RGBA() scales components to 16 bits; opaque black is r=g=b=0, a=0xffff.
			if r, g, bl, a := img.At(x, y).RGBA(); r == 0 && g == 0 && bl == 0 && a == 0xffff {
				n++
			}
		}
	}
	return n
}

func TestShapedPNGDispatchAltersRaster(t *testing.T) {
	// Same input + scale rendered three ways. ShapeSquare fills each dark cell
	// completely; ShapeRounded and ShapeDots remove foreground area, so both
	// must yield strictly fewer exact-black pixels than the square baseline.
	//
	// The level is pinned to Q for all three so the encoded matrix is identical
	// across renders (ShapeDots would otherwise raise the effective level to Q
	// on its own, producing a denser matrix and confounding the pixel counts).
	// With one shared matrix the only variable is the module shape.
	const input = "shape-dispatch"
	const scale = 8

	renderFg := func(sh qrcode.Shape) int {
		raw, err := qrcode.PNG(input,
			qrcode.WithLevel(qrcode.LevelQ),
			qrcode.WithModuleShape(sh),
			qrcode.WithScale(scale))
		if err != nil {
			t.Fatalf("PNG shape %v: %v", sh, err)
		}
		return countForeground(t, raw)
	}

	square := renderFg(qrcode.ShapeSquare)
	rounded := renderFg(qrcode.ShapeRounded)
	dots := renderFg(qrcode.ShapeDots)

	t.Logf("foreground pixel counts: square=%d rounded=%d dots=%d", square, rounded, dots)

	if square == 0 {
		t.Fatal("square render produced no foreground pixels")
	}
	if rounded >= square {
		t.Errorf("ShapeRounded fg=%d must be < ShapeSquare fg=%d (rounded corners removed no area — square fallback?)", rounded, square)
	}
	if dots >= square {
		t.Errorf("ShapeDots fg=%d must be < ShapeSquare fg=%d (circles removed no area — square fallback?)", dots, square)
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

// inFinder mirrors the unexported isEyeModule: it reports whether (x, y) lands
// in one of the three 7×7 finder patterns of a size×size matrix.
func inFinder(size, x, y int) bool {
	in := func(ox, oy int) bool { return x >= ox && x < ox+7 && y >= oy && y < oy+7 }
	return in(0, 0) || in(size-7, 0) || in(0, size-7)
}

// TestSVGDotsKeepFindersSolid proves ShapeDots shapes only data modules: the
// number of <circle> elements equals the count of dark NON-finder modules, so
// the three finder patterns are excluded from the dots and stay solid (a
// scannability requirement — decoders locate the symbol via the finders).
func TestSVGDotsKeepFindersSolid(t *testing.T) {
	const data = "svg-dots-finders"
	// Pin the level so the SVG's effective matrix (ShapeDots raises to Q) equals
	// the one Encode returns; otherwise the module counts would not line up.
	m, err := qrcode.Encode(data, qrcode.WithLevel(qrcode.LevelQ))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := qrcode.SVG(data, qrcode.WithLevel(qrcode.LevelQ), qrcode.WithModuleShape(qrcode.ShapeDots))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}

	size := m.Size()
	dataDark, totalDark := 0, 0
	for y := range size {
		for x := range size {
			if !m.Module(x, y) {
				continue
			}
			totalDark++
			if !inFinder(size, x, y) {
				dataDark++
			}
		}
	}

	circles := strings.Count(string(out), "<circle")
	if circles != dataDark {
		t.Errorf("<circle> count = %d, want %d (dark non-finder modules)", circles, dataDark)
	}
	// The finders are always dark, so excluding them must drop the count.
	if !m.Module(0, 0) {
		t.Fatal("expected finder corner (0,0) to be dark")
	}
	if circles >= totalDark {
		t.Errorf("circles=%d must be < total dark modules=%d (finders excluded)", circles, totalDark)
	}
}

// TestSVGEyeRoundedRendersRoundedFinders proves EyeRounded takes effect in SVG:
// with dotted data modules (circles), the finder cells render as rounded rects.
func TestSVGEyeRoundedRendersRoundedFinders(t *testing.T) {
	out, err := qrcode.SVG("eye-rounded",
		qrcode.WithModuleShape(qrcode.ShapeDots),
		qrcode.WithEyeShape(qrcode.EyeRounded))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `rx="0.33"`) {
		t.Error("EyeRounded SVG must render finder cells as rounded <rect rx=\"0.33\">")
	}
	if !strings.Contains(s, "<circle") {
		t.Error("dotted data modules must still render as <circle>")
	}
}
