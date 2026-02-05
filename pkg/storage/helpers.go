package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// PutFile uploads a multipart file header to storage.
// MIME type is detected from magic bytes, not the filename extension.
// Returns ErrEmptyFile if the file is nil or has zero size.
// If WithValidation is used and any rule fails, returns *FileValidationError.
func PutFile(ctx context.Context, s Storage, fh *multipart.FileHeader, opts ...Option) (*FileInfo, error) {
	if fh == nil || fh.Size == 0 {
		return nil, ErrEmptyFile
	}

	o := &putOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if len(o.validationRules) > 0 {
		mimeType := DetectMIME(fh)
		if err := ValidateFile(fh, mimeType, o.validationRules...); err != nil {
			return nil, err
		}
		// Avoid re-detecting MIME type in Put.
		opts = append(opts, WithContentType(mimeType))
	}

	f, err := fh.Open()
	if err != nil {
		return nil, fmt.Errorf("storage: failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return s.Put(ctx, f, fh.Size, opts...)
}

// PutBytes uploads byte data to storage.
// The filename is used to help with key generation but MIME type is detected from content.
func PutBytes(ctx context.Context, s Storage, data []byte, filename string, opts ...Option) (*FileInfo, error) {
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}

	r := bytes.NewReader(data)
	return s.Put(ctx, r, int64(len(data)), opts...)
}

// PutFromURL downloads a file from a URL and uploads it to storage.
// maxSize limits the download size (0 uses default from config).
// Returns ErrInvalidURL for malformed URLs.
// Returns ErrDownloadTooLarge if the file exceeds maxSize.
// Returns ErrDownloadFailed for network or HTTP errors.
func PutFromURL(ctx context.Context, s Storage, sourceURL string, maxSize int64, opts ...Option) (*FileInfo, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidURL
	}

	if maxSize == 0 {
		maxSize = DefaultMaxDownloadSize
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("%w: empty response", ErrDownloadFailed)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrDownloadFailed, resp.StatusCode)
	}

	if resp.ContentLength > maxSize {
		return nil, ErrDownloadTooLarge
	}

	// Read maxSize+1 to detect if actual size exceeds limit without buffering entire file.
	limited := io.LimitReader(resp.Body, maxSize+1)

	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}

	if int64(len(data)) > maxSize {
		return nil, ErrDownloadTooLarge
	}

	if len(data) == 0 {
		return nil, ErrEmptyFile
	}

	return s.Put(ctx, bytes.NewReader(data), int64(len(data)), opts...)
}
