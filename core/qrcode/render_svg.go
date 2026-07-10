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
// Square modules only; shaped modules and eyes are added in Task 9.
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
	writeSVGSquarePath(&b, m, c)
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

// writeSVGSquarePath emits one <path> combining every dark module as a unit
// square, offset by the quiet-zone border. Kept as its own step so a future
// shape switch (Task 9) can replace it without touching the header/background.
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
