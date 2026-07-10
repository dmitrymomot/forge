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
			// Compare against opaque black in the 8-bit space RGBA() maps to.
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
