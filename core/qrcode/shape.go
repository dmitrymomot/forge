package qrcode

// Shape styles the dark data modules when rendering to PNG or SVG.
type Shape int

const (
	ShapeSquare  Shape = iota // crisp integer-scaled blocks (default)
	ShapeRounded              // rounded-corner modules; stay connected
	ShapeDots                 // detached circles; raises level to >=Q
)

// EyeShape styles the three finder ("eye") patterns independently of the
// data modules.
type EyeShape int

const (
	EyeSquare  EyeShape = iota // default
	EyeRounded                 // rounded finder patterns
)

// isEyeModule reports whether (x, y) falls within one of the three 7x7 finder
// patterns (top-left, top-right, bottom-left) of a size×size matrix.
func isEyeModule(size, x, y int) bool {
	in := func(ox, oy int) bool { return x >= ox && x < ox+7 && y >= oy && y < oy+7 }
	return in(0, 0) || in(size-7, 0) || in(0, size-7)
}

// supersampleFactor returns the render-time oversampling factor: 1 for
// all-square rendering (crisp edges, no anti-aliasing needed), 4 when any
// curved shape (rounded modules/eyes or dots) is requested so PNG output can
// be box-downsampled for smooth edges.
func (c config) supersampleFactor() int {
	if c.moduleShape == ShapeSquare && c.eyeShape == EyeSquare {
		return 1
	}
	return 4
}
