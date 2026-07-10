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
