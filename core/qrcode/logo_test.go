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
	for y := range n {
		for x := range n {
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
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := img.Bounds()
	// Center pixel should be logo red (allow the white backing pad to miss dead
	// center by sampling the exact midpoint, which is inside the logo).
	//nolint:nilaway // img is a decoded non-palette PNG; At() never returns nil here
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	if r <= 0x8000 || g >= 0x4000 || bl >= 0x4000 {
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
