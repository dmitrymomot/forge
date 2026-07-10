package qrcode

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// colorHex renders c as a lowercase "#rrggbb" hex string, dropping alpha.
func colorHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// SVG encodes data as an SVG document. The viewBox is expressed in module
// units (including the quiet zone) so the markup scales to any display size.
//
// ShapeDots and ShapeRounded emit <circle> and <rect rx=...> elements per
// module; ShapeSquare emits the single combined <path> from Task 8. EyeShape
// is honored in PNG output only — in SVG the finder ("eye") cells render as
// ordinary modules in the requested moduleShape, so EyeRounded is currently a
// no-op refinement here (finder cells stay whatever shape moduleShape draws).
func SVG(data string, opts ...Option) ([]byte, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	m, err := encodeMatrix(data, c.effectiveLevel())
	if err != nil {
		return nil, err
	}

	full := m.Size() + 2*c.border
	var b strings.Builder

	writeSVGHeader(&b, full)
	writeSVGBackground(&b, full, c.bg)
	writeSVGModules(&b, m, c)
	if c.logo != nil {
		uri, err := logoDataURI(c.logo)
		if err != nil {
			return nil, err
		}
		side := float64(full) * c.logoSize
		pos := (float64(full) - side) / 2
		fmt.Fprintf(&b, `<image x="%.2f" y="%.2f" width="%.2f" height="%.2f" href="%s"/>`,
			pos, pos, side, side, uri)
	}
	b.WriteString(`</svg>`)

	return []byte(b.String()), nil
}

// writeSVGHeader opens the <svg> element with a viewBox in module units
// (side length full, including the quiet zone) and a crisp-edges hint.
func writeSVGHeader(b *strings.Builder, full int) {
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	b.WriteString(strconv.Itoa(full))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(full))
	b.WriteString(`" shape-rendering="crispEdges">`)
}

// writeSVGBackground draws a full×full <rect> filled with bg, covering the
// quiet zone and the matrix.
func writeSVGBackground(b *strings.Builder, full int, bg color.Color) {
	fmt.Fprintf(b, `<rect width="%d" height="%d" fill="%s"/>`, full, full, colorHex(bg))
}

// writeSVGModules emits the dark modules in the shape requested by
// c.moduleShape: circles for ShapeDots, rounded rects for ShapeRounded, or
// one combined <path> of unit squares for ShapeSquare (the default, cheapest
// to render since it needs no per-module element).
func writeSVGModules(b *strings.Builder, m *Matrix, c config) {
	switch c.moduleShape {
	case ShapeDots:
		writeSVGDots(b, m, c)
	case ShapeRounded:
		writeSVGRounded(b, m, c)
	default:
		writeSVGSquarePath(b, m, c)
	}
}

// writeSVGDots emits one <circle> per dark module, inscribed in its unit
// cell and offset by the quiet-zone border.
func writeSVGDots(b *strings.Builder, m *Matrix, c config) {
	dark := colorHex(c.fg)
	for y := range m.Size() {
		for x := range m.Size() {
			if m.Module(x, y) {
				fmt.Fprintf(b, `<circle cx="%.1f" cy="%.1f" r="0.5" fill="%s"/>`,
					float64(x+c.border)+0.5, float64(y+c.border)+0.5, dark)
			}
		}
	}
}

// writeSVGRounded emits one <rect rx="0.33"> per dark module, offset by the
// quiet-zone border.
func writeSVGRounded(b *strings.Builder, m *Matrix, c config) {
	dark := colorHex(c.fg)
	for y := range m.Size() {
		for x := range m.Size() {
			if m.Module(x, y) {
				fmt.Fprintf(b, `<rect x="%d" y="%d" width="1" height="1" rx="0.33" fill="%s"/>`,
					x+c.border, y+c.border, dark)
			}
		}
	}
}

// writeSVGSquarePath emits one <path> combining every dark module as a unit
// square, offset by the quiet-zone border. One combined path is cheaper than
// per-module <rect> elements and keeps crisp edges via shape-rendering.
func writeSVGSquarePath(b *strings.Builder, m *Matrix, c config) {
	b.WriteString(`<path fill="`)
	b.WriteString(colorHex(c.fg))
	b.WriteString(`" d="`)
	for y := range m.Size() {
		for x := range m.Size() {
			if m.Module(x, y) {
				fmt.Fprintf(b, "M%d %dh1v1h-1z", x+c.border, y+c.border)
			}
		}
	}
	b.WriteString(`"/>`)
}
