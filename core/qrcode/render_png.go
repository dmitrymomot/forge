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
	for y := range m.Size() {
		for x := range m.Size() {
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
