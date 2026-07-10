package qrcode

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// colorHex renders c as a lowercase "#rrggbb" hex string, dropping alpha.
// Translucency is carried separately via a fill-opacity attribute (see svgFill).
// The channels come from color.RGBA(), which is alpha-premultiplied, so a
// translucent saturated color is darkened; foreground/background are expected
// to be opaque (solid), which the package targets and renders exactly.
func colorHex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// svgFill formats the SVG fill for c: `fill="#rrggbb"` for opaque colors, plus a
// `fill-opacity="a"` attribute when c is translucent (alpha < 0xffff). Opaque
// colors emit no opacity, so their markup is byte-identical to plain colorHex.
func svgFill(c color.Color) string {
	_, _, _, a := c.RGBA()
	hex := colorHex(c)
	if a >= 0xffff {
		return `fill="` + hex + `"`
	}
	op := strconv.FormatFloat(float64(a)/0xffff, 'g', 4, 64)
	return `fill="` + hex + `" fill-opacity="` + op + `"`
}

// SVG encodes data as an SVG document. The viewBox is expressed in module
// units (including the quiet zone) so the markup scales to any display size.
//
// Data modules follow c.moduleShape: ShapeDots emits a <circle> per module,
// ShapeRounded a rounded <rect rx=...>, ShapeSquare a combined <path> of unit
// squares. The three finder ("eye") patterns follow c.eyeShape independently
// and always render solid — a square (EyeSquare) or rounded square (EyeRounded),
// never shattered into dots — so a decoder can still locate the symbol by its
// finder ratio. This mirrors the PNG renderer. When both shapes are square
// (the default) every module is a plain square and the whole matrix is a single
// combined <path>.
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

	writeSVGHeader(&b, full, c.logo != nil)
	writeSVGBackground(&b, full, c.bg)
	writeSVGModules(&b, m, c)
	if c.logo != nil {
		if err := writeSVGLogo(&b, full, c); err != nil {
			return nil, err
		}
	}
	b.WriteString(`</svg>`)

	return []byte(b.String()), nil
}

// writeSVGLogo emits a solid backing <rect> (in c.bg, mirroring the PNG white
// pad) followed by the base64 <image>, both centered and sized to c.logoSize of
// the full symbol. The backing rect keeps a transparent logo from letting dark
// modules bleed through and matches the PNG compositing pad.
func writeSVGLogo(b *strings.Builder, full int, c config) error {
	uri, err := logoDataURI(c.logo)
	if err != nil {
		return err
	}
	side := float64(full) * c.logoSize
	pos := (float64(full) - side) / 2
	pad := side / 8
	fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" %s/>`,
		pos-pad, pos-pad, side+2*pad, side+2*pad, svgFill(c.bg))
	// xlink:href duplicates href for older SVG1.1 renderers (older Safari, some
	// email clients) that don't resolve the SVG2 bare href attribute.
	fmt.Fprintf(b, `<image x="%.2f" y="%.2f" width="%.2f" height="%.2f" href="%s" xlink:href="%s"/>`,
		pos, pos, side, side, uri, uri)
	return nil
}

// writeSVGHeader opens the <svg> element with a viewBox in module units
// (side length full, including the quiet zone) and a crisp-edges hint. The
// xlink namespace is only declared when withLogo is true, so logo-less output
// stays byte-identical to before the xlink:href fallback was added.
func writeSVGHeader(b *strings.Builder, full int, withLogo bool) {
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg"`)
	if withLogo {
		b.WriteString(` xmlns:xlink="http://www.w3.org/1999/xlink"`)
	}
	b.WriteString(` viewBox="0 0 `)
	b.WriteString(strconv.Itoa(full))
	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(full))
	b.WriteString(`" shape-rendering="crispEdges">`)
}

// writeSVGBackground draws a full×full <rect> filled with bg, covering the
// quiet zone and the matrix.
func writeSVGBackground(b *strings.Builder, full int, bg color.Color) {
	fmt.Fprintf(b, `<rect width="%d" height="%d" %s/>`, full, full, svgFill(bg))
}

// writeSVGModules emits the dark modules. Data modules use c.moduleShape; the
// three finder ("eye") patterns use c.eyeShape and stay solid. Plain squares
// (square data modules and square eyes) are batched into one combined <path>;
// circles and rounded rects are emitted as individual elements.
func writeSVGModules(b *strings.Builder, m *Matrix, c config) {
	// Fast path: every module is a plain square → one combined <path>.
	if c.moduleShape == ShapeSquare && c.eyeShape == EyeSquare {
		writeSVGSquarePath(b, m, c)
		return
	}
	writeSVGShapedModules(b, m, c)
}

// writeSVGSquarePath emits one <path> combining every dark module as a unit
// square, offset by the quiet-zone border. One combined path is cheaper than
// per-module <rect> elements and keeps crisp edges via shape-rendering.
func writeSVGSquarePath(b *strings.Builder, m *Matrix, c config) {
	b.WriteString(`<path `)
	b.WriteString(svgFill(c.fg))
	b.WriteString(` d="`)
	for y := range m.Size() {
		for x := range m.Size() {
			if m.Module(x, y) {
				fmt.Fprintf(b, "M%d %dh1v1h-1z", x+c.border, y+c.border)
			}
		}
	}
	b.WriteString(`"/>`)
}

// writeSVGShapedModules renders a mix of square and curved modules. Squares are
// accumulated into one combined <path>; circles (ShapeDots data) and rounded
// rects (ShapeRounded data or EyeRounded finders) are emitted individually.
func writeSVGShapedModules(b *strings.Builder, m *Matrix, c config) {
	fill := svgFill(c.fg)
	size := m.Size()
	var squares, curved strings.Builder
	for y := range size {
		for x := range size {
			if !m.Module(x, y) {
				continue
			}
			px, py := x+c.border, y+c.border
			if isEyeModule(size, x, y) {
				// Finder cells stay solid, shaped only by eyeShape.
				if c.eyeShape == EyeRounded {
					fmt.Fprintf(&curved, `<rect x="%d" y="%d" width="1" height="1" rx="0.33" %s/>`, px, py, fill)
				} else {
					fmt.Fprintf(&squares, "M%d %dh1v1h-1z", px, py)
				}
				continue
			}
			switch c.moduleShape {
			case ShapeDots:
				fmt.Fprintf(&curved, `<circle cx="%.1f" cy="%.1f" r="0.5" %s/>`, float64(px)+0.5, float64(py)+0.5, fill)
			case ShapeRounded:
				fmt.Fprintf(&curved, `<rect x="%d" y="%d" width="1" height="1" rx="0.33" %s/>`, px, py, fill)
			default:
				fmt.Fprintf(&squares, "M%d %dh1v1h-1z", px, py)
			}
		}
	}
	if squares.Len() > 0 {
		b.WriteString(`<path `)
		b.WriteString(fill)
		b.WriteString(` d="`)
		b.WriteString(squares.String())
		b.WriteString(`"/>`)
	}
	b.WriteString(curved.String())
}
