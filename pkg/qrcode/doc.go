// Package qrcode generates QR code images from string content.
//
// It produces PNG-encoded QR codes as raw bytes or base64 data URIs,
// using medium error correction (recovers up to 15% damage). The default
// image size is 256x256 pixels.
//
// # Basic Usage
//
// Generate a QR code as PNG bytes:
//
//	import "github.com/dmitrymomot/forge/pkg/qrcode"
//
//	png, err := qrcode.Generate("https://example.com")
//	if err != nil {
//		log.Fatal(err)
//	}
//	// png contains the raw PNG image bytes
//
// Generate a data URI for embedding in HTML:
//
//	dataURI, err := qrcode.GenerateBase64Image("https://example.com")
//	if err != nil {
//		log.Fatal(err)
//	}
//	// use in HTML: <img src="{{dataURI}}">
//
// # Custom Size
//
// Pass an optional size in pixels:
//
//	png, err := qrcode.Generate("https://example.com", 512)
//
// # TOTP Integration
//
// Pair with the totp package to generate authenticator app QR codes:
//
//	import (
//		"github.com/dmitrymomot/forge/pkg/qrcode"
//		"github.com/dmitrymomot/forge/pkg/totp"
//	)
//
//	secret, err := totp.GenerateSecretKey()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	uri, err := totp.GetTOTPURI(totp.TOTPParams{
//		Secret:      secret,
//		AccountName: "user@example.com",
//		Issuer:      "MyApp",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	dataURI, err := qrcode.GenerateBase64Image(uri)
//	if err != nil {
//		log.Fatal(err)
//	}
//	// Render dataURI in an <img> tag for the user to scan
//
// # Error Handling
//
// The package returns [ErrEmptyContent] for empty or whitespace-only input
// and [ErrFailedToGenerateQRCode] for encoding failures:
//
//	png, err := qrcode.Generate("")
//	if errors.Is(err, qrcode.ErrEmptyContent) {
//		// handle empty content
//	}
package qrcode
