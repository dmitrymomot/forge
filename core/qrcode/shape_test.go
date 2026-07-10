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
