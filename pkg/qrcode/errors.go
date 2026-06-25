package qrcode

import "errors"

var (
	ErrEmptyContent           = errors.New("qr code content must not be empty")
	ErrSizeTooLarge           = errors.New("qr code size exceeds maximum allowed")
	ErrFailedToGenerateQRCode = errors.New("failed to generate QR code")
)
