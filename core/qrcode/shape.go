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
