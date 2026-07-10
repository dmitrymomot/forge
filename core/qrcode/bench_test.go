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
