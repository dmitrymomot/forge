package qrcode

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// compositeLogo scales c.logo to c.logoSize of the base image and draws it
// centered over a backing pad in c.bg. Semi-transparent logo pixels are
// alpha-blended (source-over) onto the pad so anti-aliased edges soften into it;
// opaque pixels overwrite it exactly. No-op when no logo is set.
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
	// Exactly side×side, so an odd side keeps its full width.
	x0, y0 := cx-side/2, cy-side/2
	// Backing pad in the background color.
	padRect := image.Rect(x0-pad, y0-pad, x0+side+pad, y0+side+pad)
	draw.Draw(base, padRect, image.NewUniform(c.bg), image.Point{}, draw.Src)
	// Nearest-neighbor scale the logo into the target rect, blending over the pad.
	dst := image.Rect(x0, y0, x0+side, y0+side)
	src := c.logo.Bounds()
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		for x := dst.Min.X; x < dst.Max.X; x++ {
			sx := src.Min.X + (x-dst.Min.X)*src.Dx()/side
			sy := src.Min.Y + (y-dst.Min.Y)*src.Dy()/side
			lp := c.logo.At(sx, sy)
			if lp == nil {
				continue // empty-palette pixel: leave the backing pad
			}
			base.Set(x, y, blendOver(lp, base.At(x, y)))
		}
	}
}

// blendOver composites src over dst using premultiplied source-over alpha.
// A fully opaque src returns src unchanged; a fully transparent src returns dst.
func blendOver(src, dst color.Color) color.Color {
	sr, sg, sb, sa := src.RGBA()
	if sa == 0xffff {
		return src
	}
	dr, dg, db, da := dst.RGBA()
	inv := 0xffff - sa
	return color.RGBA64{
		R: uint16(sr + dr*inv/0xffff),
		G: uint16(sg + dg*inv/0xffff),
		B: uint16(sb + db*inv/0xffff),
		A: uint16(sa + da*inv/0xffff),
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
