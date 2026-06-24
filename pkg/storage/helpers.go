package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"syscall"
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
// The filename is used to seed the generated key's extension and the
// Content-Disposition-friendly name when no explicit content type or key is
// provided; the MIME type itself is always detected from content, not the
// filename. Caller-supplied options take precedence over the filename hint.
func PutBytes(ctx context.Context, s Storage, data []byte, filename string, opts ...Option) (*FileInfo, error) {
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}

	// Seed the key extension from the filename so the generated key keeps a
	// sensible suffix even when content sniffing is inconclusive. This is
	// prepended so explicit caller options still win.
	if ext := extFromFilename(filename); ext != "" {
		opts = append([]Option{withFilenameExt(ext)}, opts...)
	}

	r := bytes.NewReader(data)
	return s.Put(ctx, r, int64(len(data)), opts...)
}

// PutFromURL downloads a file from a URL and uploads it to storage.
// maxSize limits the download size (0 uses default from config).
// Returns ErrInvalidURL for malformed URLs.
// Returns ErrDownloadTooLarge if the file exceeds maxSize.
// Returns ErrDownloadFailed for network or HTTP errors.
//
// # Trust boundary (SSRF)
//
// sourceURL is treated as untrusted. By default PutFromURL refuses to connect
// to private, loopback, link-local, or unspecified addresses, so a caller
// passing an attacker-controlled URL cannot coerce the server into requesting
// internal endpoints (cloud metadata, localhost services, RFC1918 hosts). The
// check is enforced at dial time on the resolved IP, which also defends against
// DNS-rebinding (a public hostname that resolves to a private IP). Returns
// ErrDownloadFailed (wrapping ErrBlockedAddress) when a blocked destination is
// reached.
//
// Callers that legitimately need to fetch from internal URLs (e.g. trusted
// service-to-service transfers) can opt out with WithAllowPrivateURL.
func PutFromURL(ctx context.Context, s Storage, sourceURL string, maxSize int64, opts ...Option) (*FileInfo, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidURL
	}

	if maxSize == 0 {
		maxSize = DefaultMaxDownloadSize
	}

	o := &putOptions{}
	for _, opt := range opts {
		opt(o)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	if !o.allowPrivateURL {
		client.Transport = ssrfSafeTransport()
	}
	resp, err := client.Do(req)
	if err != nil {
		// Preserve the blocked-address cause so callers can distinguish an SSRF
		// rejection from a generic network failure via errors.Is.
		if errors.Is(err, ErrBlockedAddress) {
			return nil, fmt.Errorf("%w: %w", ErrDownloadFailed, ErrBlockedAddress)
		}
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

// ssrfSafeTransport returns an http.Transport whose dialer rejects connections
// to private, loopback, link-local, or unspecified IP addresses. The check runs
// on the address actually resolved/dialed, so it also blocks DNS-rebinding
// where a public hostname resolves to an internal IP.
func ssrfSafeTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%w: %v", ErrBlockedAddress, err)
			}
			ip := net.ParseIP(host)
			if ip == nil || isBlockedIP(ip) {
				return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
			}
			return nil
		},
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	return transport
}

// isBlockedIP reports whether an IP is in a range that must not be reachable
// from a user-supplied URL (SSRF protection). It blocks loopback, private
// (RFC1918 / RFC4193), link-local, and unspecified addresses, plus the IPv4
// cloud-metadata mapping behind IPv4-in-IPv6.
func isBlockedIP(ip net.IP) bool {
	// Normalize IPv4-mapped IPv6 (e.g. ::ffff:169.254.169.254) to IPv4 so the
	// range checks below apply uniformly.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// extFromFilename extracts a sanitized lowercase extension (including the dot)
// from a filename, e.g. "photo.JPG" -> ".jpg". Returns "" when there is no
// usable extension.
func extFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if ext == "" || ext == "." {
		return ""
	}
	// Only allow simple, safe extensions to avoid smuggling path/control
	// characters into the generated key.
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return ""
		}
	}
	return ext
}
