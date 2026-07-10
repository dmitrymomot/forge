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

// TestPNGTranslucentLogoBlends proves a semi-transparent logo pixel is
// alpha-blended over the backing pad rather than raw-overwritten: blending a
// 50%-alpha black logo onto the white pad yields an OPAQUE mid-grey center. A
// raw overwrite would instead store a semi-transparent (alpha≈128) pixel.
func TestPNGTranslucentLogoBlends(t *testing.T) {
	logo := solidLogo(40, color.NRGBA{R: 0, G: 0, B: 0, A: 128})
	raw, err := qrcode.PNG("https://forge.example/logo", qrcode.WithScale(8), qrcode.WithLogo(logo))
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	b := img.Bounds()
	//nolint:nilaway // img is a decoded non-palette PNG; At() never returns nil here
	r, g, bl, a := img.At(b.Dx()/2, b.Dy()/2).RGBA()
	if a != 0xffff {
		t.Errorf("blended center must be opaque (a=%d), got alpha %d — logo raw-overwrote the pad?", 0xffff, a>>8)
	}
	// White pad + 50% black ≈ mid grey; assert it is neither white nor black.
	if r < 0x4000 || r > 0xc000 || g < 0x4000 || g > 0xc000 || bl < 0x4000 || bl > 0xc000 {
		t.Errorf("center not blended mid-grey: r=%d g=%d b=%d", r>>8, g>>8, bl>>8)
	}
}

func TestSVGLogoEmbedsImage(t *testing.T) {
	logo := solidLogo(16, color.RGBA{B: 255, A: 255})
	out, err := qrcode.SVG("hello", qrcode.WithLogo(logo))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "<image") || !strings.Contains(s, "data:image/png;base64,") {
		t.Error("SVG logo must embed a base64 <image>")
	}
	if !strings.Contains(s, `xmlns:xlink="http://www.w3.org/1999/xlink"`) {
		t.Error("SVG with logo must declare the xlink namespace")
	}
	if !strings.Contains(s, `xlink:href="data:image/png;base64,`) {
		t.Error("SVG logo <image> must carry an xlink:href fallback matching href")
	}
}

// TestSVGWithoutLogoOmitsXlinkNamespace is a regression guard: logo-less SVG
// output must stay byte-identical to before this fix, so the xlink namespace
// must only appear when a logo is actually rendered.
func TestSVGWithoutLogoOmitsXlinkNamespace(t *testing.T) {
	out, err := qrcode.SVG("hello")
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	if strings.Contains(string(out), "xlink") {
		t.Errorf("logo-less SVG must not mention xlink: %.120q", string(out))
	}
}

// TestSVGLogoHasBackingRect proves the SVG logo is drawn over a solid backing
// rect (mirroring the PNG white pad) so a transparent logo cannot let dark
// modules bleed through. With default square modules the matrix is a single
// <path>, so the only rects are the background plus the logo backing = 2.
func TestSVGLogoHasBackingRect(t *testing.T) {
	logo := solidLogo(16, color.NRGBA{R: 255, A: 128})
	out, err := qrcode.SVG("hello", qrcode.WithLogo(logo))
	if err != nil {
		t.Fatalf("SVG: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "<rect"); n != 2 {
		t.Errorf("<rect> count = %d, want 2 (background + logo backing pad)", n)
	}
	if !strings.Contains(s, "<image") {
		t.Error("SVG logo must still embed an <image>")
	}
}
