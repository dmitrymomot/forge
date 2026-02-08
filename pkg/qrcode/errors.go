package qrcode

import "errors"

var (
	ErrEmptyContent           = errors.New("qr code content must not be empty")
	ErrFailedToGenerateQRCode = errors.New("failed to generate QR code")
)
