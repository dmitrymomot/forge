package qrcode

import "errors"

var (
	// ErrTooLarge reports that the input exceeds the byte-mode capacity of the
	// largest QR version (40) at the effective error-correction level.
	ErrTooLarge = errors.New("qrcode: data too large for a QR code")
	// ErrInvalidScale reports a non-positive WithScale value.
	ErrInvalidScale = errors.New("qrcode: scale must be positive")
	// ErrInvalidBorder reports a negative WithBorder value.
	ErrInvalidBorder = errors.New("qrcode: border must be non-negative")
	// ErrInvalidLogoSize reports a WithLogoSize outside (0, 0.3].
	ErrInvalidLogoSize = errors.New("qrcode: logo size must be within (0, 0.3]")
)
