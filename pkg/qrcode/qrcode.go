package qrcode

import (
	"encoding/base64"
	"fmt"
	"strings"

	goqrcode "github.com/skip2/go-qrcode"
)

const (
	defaultSize = 256
	// maxSize caps the requested image width/height in pixels. A QR code is a
	// square, so the rendered PNG allocates on the order of size*size pixels;
	// without a bound, a caller-supplied size is an unbounded-allocation
	// surface. 4096 is comfortably larger than any practical on-screen or
	// print use while keeping the worst-case allocation bounded.
	maxSize = 4096
)

// Generate encodes content into a QR code and returns PNG image bytes.
// The optional size parameter sets the image width and height in pixels
// (default 256, maximum 4096). Sizes above the maximum return
// [ErrSizeTooLarge]; non-positive sizes fall back to the default.
// Uses medium error correction (recovers up to 15% damage).
func Generate(content string, size ...int) ([]byte, error) {
	if err := validateContent(content); err != nil {
		return nil, err
	}

	resolved, err := resolveSize(size...)
	if err != nil {
		return nil, err
	}

	png, err := goqrcode.Encode(content, goqrcode.Medium, resolved)
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

func resolveSize(size ...int) (int, error) {
	if len(size) == 0 || size[0] <= 0 {
		return defaultSize, nil
	}
	if size[0] > maxSize {
		return 0, fmt.Errorf("%w: %d (maximum %d)", ErrSizeTooLarge, size[0], maxSize)
	}
	return size[0], nil
}
