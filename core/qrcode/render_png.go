package qrcode

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// resolveScale returns pixels-per-module: WithSize (largest integer scale whose
// full image fits the target) wins over WithScale when set.
func resolveScale(c config, fullModules int) int {
	if c.targetSize > 0 {
		s := c.targetSize / fullModules
		return max(s, 1)
	}
	return c.scale
}

// renderImage draws the matrix with quiet-zone border and fg/bg colors at the
// resolved integer scale, honoring the requested module/eye shapes. Curved
// shapes are rendered at supersampleFactor()x resolution and box-downsampled
// for anti-aliased edges; all-square rendering skips supersampling entirely.
func renderImage(m *Matrix, c config) *image.RGBA {
	full := m.Size() + 2*c.border
	scale := resolveScale(c, full)
	ss := c.supersampleFactor()
	hi := image.NewRGBA(image.Rect(0, 0, full*scale*ss, full*scale*ss))
	draw.Draw(hi, hi.Bounds(), image.NewUniform(c.bg), image.Point{}, draw.Src)
	cell := scale * ss
	for y := range m.Size() {
		for x := range m.Size() {
			if !m.Module(x, y) {
				continue
			}
			x0 := (x + c.border) * cell
			y0 := (y + c.border) * cell
			if isEyeModule(m.Size(), x, y) {
				drawEyeModule(hi, x0, y0, cell, c)
				continue
			}
			drawModule(hi, x0, y0, cell, c.moduleShape, c.fg)
		}
	}
	if ss == 1 {
		return hi
	}
	return downsample(hi, ss)
}

// drawModule fills one module cell in the requested shape.
func drawModule(img *image.RGBA, x0, y0, cell int, shape Shape, fg color.Color) {
	switch shape {
	case ShapeDots:
		fillCircle(img, x0, y0, cell, fg)
	case ShapeRounded:
		fillRounded(img, x0, y0, cell, cell/3, fg)
	default:
		draw.Draw(img, image.Rect(x0, y0, x0+cell, y0+cell), image.NewUniform(fg), image.Point{}, draw.Src)
	}
}

// drawEyeModule fills a finder-pattern cell. EyeRounded renders every cell
// of the three finder patterns as rounded squares so the eye reads as one
// smoothly-cornered mark; EyeSquare keeps crisp corners regardless of the
// data-module shape.
func drawEyeModule(img *image.RGBA, x0, y0, cell int, c config) {
	shape := ShapeSquare
	if c.eyeShape == EyeRounded {
		shape = ShapeRounded
	}
	drawModule(img, x0, y0, cell, shape, c.fg)
}

// fillCircle draws a filled circle inscribed in the cell×cell square whose
// top-left corner is (x0, y0).
func fillCircle(img *image.RGBA, x0, y0, cell int, fg color.Color) {
	r := float64(cell) / 2
	cx, cy := float64(x0)+r, float64(y0)+r
	for py := y0; py < y0+cell; py++ {
		for px := x0; px < x0+cell; px++ {
			dx, dy := float64(px)+0.5-cx, float64(py)+0.5-cy
			if dx*dx+dy*dy <= r*r {
				img.Set(px, py, fg)
			}
		}
	}
}

// fillRounded draws a filled square with corners rounded to radius rad,
// inscribed in the cell×cell square whose top-left corner is (x0, y0).
func fillRounded(img *image.RGBA, x0, y0, cell, rad int, fg color.Color) {
	rf := float64(rad)
	for py := y0; py < y0+cell; py++ {
		for px := x0; px < x0+cell; px++ {
			lx := float64(px - x0)
			ly := float64(py - y0)
			// Nearest corner-arc center check.
			cxx, cyy := lx, ly
			switch {
			case lx < rf:
				cxx = rf
			case lx > float64(cell)-rf:
				cxx = float64(cell) - rf
			}
			switch {
			case ly < rf:
				cyy = rf
			case ly > float64(cell)-rf:
				cyy = float64(cell) - rf
			}
			dx, dy := lx-cxx, ly-cyy
			if dx*dx+dy*dy <= rf*rf {
				img.Set(px, py, fg)
			}
		}
	}
}

// downsample box-averages each ss×ss block of src into one pixel of the
// returned image, producing anti-aliased edges for supersampled curves.
func downsample(src *image.RGBA, ss int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx()/ss, b.Dy()/ss
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	area := uint32(ss * ss)
	for y := range h {
		for x := range w {
			var sr, sg, sb, sa uint32
			for dy := range ss {
				for dx := range ss {
					r, g, bl, a := src.At(x*ss+dx, y*ss+dy).RGBA()
					sr += r >> 8
					sg += g >> 8
					sb += bl >> 8
					sa += a >> 8
				}
			}
			dst.Set(x, y, color.RGBA{uint8(sr / area), uint8(sg / area), uint8(sb / area), uint8(sa / area)})
		}
	}
	return dst
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
	compositeLogo(img, c)
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
