package qrcode

import (
	"encoding/base64"
	"fmt"
	"strings"

	goqrcode "github.com/skip2/go-qrcode"
)

const defaultSize = 256

// Generate encodes content into a QR code and returns PNG image bytes.
// The optional size parameter sets the image width and height in pixels (default 256).
// Uses medium error correction (recovers up to 15% damage).
func Generate(content string, size ...int) ([]byte, error) {
	if err := validateContent(content); err != nil {
		return nil, err
	}

	png, err := goqrcode.Encode(content, goqrcode.Medium, resolveSize(size...))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToGenerateQRCode, err)
	}

	return png, nil
}

// GenerateBase64Image encodes content into a QR code and returns a data URI
// string suitable for use in an HTML <img src=""> attribute.
// The optional size parameter sets the image width and height in pixels (default 256).
func GenerateBase64Image(content string, size ...int) (string, error) {
	png, err := Generate(content, size...)
	if err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(png)
	return "data:image/png;base64," + encoded, nil
}

func validateContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return ErrEmptyContent
	}
	return nil
}

func resolveSize(size ...int) int {
	if len(size) > 0 && size[0] > 0 {
		return size[0]
	}
	return defaultSize
}
