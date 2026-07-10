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
	m, err := qrcode.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
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
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	// Top-left pixel is in the quiet zone → background (blue-ish).
	//nolint:nilaway // img is a decoded non-palette PNG; At() never returns nil here
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
	m, err := qrcode.Encode(data)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	full := m.Size() + 2*4 // modules + default border on both sides
	raw, err := qrcode.PNG(data, qrcode.WithSize(full*10+5))
	if err != nil {
		t.Fatalf("PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if img.Bounds().Dx() != full*10 { // largest integer scale that fits target
		t.Errorf("size = %d, want %d", img.Bounds().Dx(), full*10)
	}
}
