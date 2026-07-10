// Package qrcode generates QR codes from any string with no external
// dependencies. It exposes the raw module matrix plus PNG, base64 PNG
// data-URI, and SVG renderers, with configurable size, colors, a center logo,
// and styled module/eye shapes.
//
// The encoder is byte mode only (UTF-8) with automatic version selection
// (1–40) and error-correction levels L/M/Q/H (default M). Setting a logo or
// ShapeDots raises the effective level to at least Q so the result stays
// scannable.
//
// # Usage
//
//	// A 2FA enrollment QR as an <img src> value.
//	uri, err := qrcode.DataURI("otpauth://totp/App:user?secret=ABC&issuer=App")
//
//	// A branded referral code: high error correction, rounded modules, logo.
//	png, err := qrcode.PNG(link,
//		qrcode.WithLevel(qrcode.LevelH),
//		qrcode.WithModuleShape(qrcode.ShapeRounded),
//		qrcode.WithLogo(logoImg),
//		qrcode.WithSize(512),
//	)
//
//	// The raw grid, to render your own way.
//	m, err := qrcode.Encode(link)
//	for y := 0; y < m.Size(); y++ {
//		for x := 0; x < m.Size(); x++ {
//			_ = m.Module(x, y) // true = dark
//		}
//	}
package qrcode
