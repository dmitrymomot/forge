// Package qrcode generates QR codes from any string with no external
// dependencies. It exposes the raw module matrix plus PNG, base64 PNG
// data-URI, and SVG renderers, with configurable size, colors, a center logo,
// and styled module/eye shapes.
//
// The encoder is byte mode only (UTF-8) with automatic version selection
// (1–40) and error-correction levels L/M/Q/H (default M). When PNG, SVG, or
// DataURI render a logo or ShapeDots, they raise the effective error-correction
// level to at least Q so the result stays scannable; Encode ignores render
// options and always uses the level you pass.
//
// # Usage
//
//	var link string // e.g. a short link or otpauth:// URI
//	var logo image.Image // decoded PNG/JPEG, e.g. via image.Decode
//
//	// A branded QR: high error correction, rounded modules, centered logo.
//	png, err := qrcode.PNG(link,
//		qrcode.WithLevel(qrcode.LevelH),
//		qrcode.WithModuleShape(qrcode.ShapeRounded),
//		qrcode.WithLogo(logo),
//	)
//
//	// The raw grid, to render your own way.
//	m, err := qrcode.Encode(link)
//	dark := m.Module(0, 0) // true = dark; iterate [0, m.Size()) for the rest
package qrcode
