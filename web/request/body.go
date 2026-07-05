package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"

	"github.com/dmitrymomot/forge/core/filetype"
)

const (
	defaultMaxBytes        = 1 << 20  // 1 MiB — DecodeJSON / RawBody body cap
	defaultMultipartMemory = 32 << 20 // 32 MiB — File / Files in-memory cap
)

type bodyConfig struct {
	maxBytes     int64
	maxBytesSet  bool
	allowUnknown bool
	skipCType    bool
}

// BodyOption configures DecodeJSON, RawBody, File, and Files.
type BodyOption func(*bodyConfig)

// WithMaxBytes sets the body size cap for every body reader; n <= 0 disables the
// limit for DecodeJSON/RawBody (and falls back to 32 MiB for File/Files memory).
func WithMaxBytes(n int64) BodyOption {
	return func(c *bodyConfig) { c.maxBytes = n; c.maxBytesSet = true }
}

// AllowUnknownFields turns off DisallowUnknownFields (DecodeJSON only).
func AllowUnknownFields() BodyOption {
	return func(c *bodyConfig) { c.allowUnknown = true }
}

// SkipContentType accepts any/absent Content-Type (DecodeJSON only).
func SkipContentType() BodyOption {
	return func(c *bodyConfig) { c.skipCType = true }
}

func newBodyConfig(opts []BodyOption) bodyConfig {
	var c bodyConfig
	for _, o := range opts {
		o(&c)
	}
	return c
}

// limitedBody wraps r.Body in a MaxBytesReader using the config's cap (default
// 1 MiB; a non-positive explicit cap disables the limit). The nil ResponseWriter
// is fine: MaxBytesReader only type-asserts w to signal the server, which we don't
// need here.
func limitedBody(r *http.Request, c bodyConfig) io.ReadCloser {
	limit := int64(defaultMaxBytes)
	if c.maxBytesSet {
		limit = c.maxBytes
	}
	if limit <= 0 {
		return r.Body
	}
	return http.MaxBytesReader(nil, r.Body, limit)
}

// matchesMediaType reports whether header's media type equals media
// (case-insensitive, parameters ignored).
func matchesMediaType(header, media string) bool {
	if header == "" {
		return false
	}
	got, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	want, _, err := mime.ParseMediaType(media)
	if err != nil {
		want = media
	}
	return strings.EqualFold(got, want)
}

// decodeError classifies a body read/decode failure: an *http.MaxBytesError maps
// to KindTooLarge, anything else to KindInvalidBody.
func decodeError(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return &Error{Source: SourceBody, Kind: KindTooLarge, Err: err}
	}
	return &Error{Source: SourceBody, Kind: KindInvalidBody, Err: err}
}

// DecodeJSON strictly decodes the JSON request body into dst. Defaults: 1 MiB cap
// (-> 413), require Content-Type application/json (-> 415), DisallowUnknownFields
// and reject trailing data and empty bodies (-> 400). Override with options.
func DecodeJSON(r *http.Request, dst any, opts ...BodyOption) error {
	c := newBodyConfig(opts)

	if !c.skipCType {
		if ct := r.Header.Get("Content-Type"); !matchesMediaType(ct, "application/json") {
			return &Error{
				Source: SourceBody,
				Kind:   KindUnsupportedMediaType,
				Err:    fmt.Errorf("content-type %q is not application/json", ct),
			}
		}
	}

	dec := json.NewDecoder(limitedBody(r, c))
	if !c.allowUnknown {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if dec.More() {
		return &Error{Source: SourceBody, Kind: KindInvalidBody, Err: errors.New("unexpected data after JSON value")}
	}
	return nil
}

// IsContentType reports whether the request's Content-Type media type equals media
// (case-insensitive, parameters ignored). It inspects the request's own
// Content-Type, not the Accept header — it is not content negotiation.
func IsContentType(r *http.Request, media string) bool {
	return matchesMediaType(r.Header.Get("Content-Type"), media)
}

// RawBody reads the entire request body into a byte slice under the configured
// size cap (default 1 MiB; overflow -> 413). For webhook HMAC verification and
// arbitrary payloads. No Content-Type check.
func RawBody(r *http.Request, opts ...BodyOption) ([]byte, error) {
	c := newBodyConfig(opts)
	b, err := io.ReadAll(limitedBody(r, c))
	if err != nil {
		return nil, decodeError(err)
	}
	return b, nil
}

// File returns the first uploaded file for key. The caller must Close the returned
// multipart.File. A non-multipart request -> 415. An absent key -> 400 with Kind
// KindMissing and Err http.ErrMissingFile (use errors.Is(err, http.ErrMissingFile)
// to distinguish absence from a bad upload, which is KindMalformed).
func File(r *http.Request, key string, opts ...BodyOption) (multipart.File, *multipart.FileHeader, error) {
	if err := parseMultipart(r, opts); err != nil {
		return nil, nil, err
	}
	f, h, err := r.FormFile(key)
	if err != nil {
		kind := KindMalformed
		if errors.Is(err, http.ErrMissingFile) {
			kind = KindMissing // absent key, not an unparseable value
		}
		return nil, nil, &Error{Source: SourceForm, Key: key, Kind: kind, Err: err}
	}
	return f, h, nil
}

// Files returns every uploaded file header for key (open each via fh.Open()). An
// absent key -> 400 with Kind KindMissing and Err http.ErrMissingFile, mirroring
// File so callers can errors.Is(err, http.ErrMissingFile) for either.
func Files(r *http.Request, key string, opts ...BodyOption) ([]*multipart.FileHeader, error) {
	if err := parseMultipart(r, opts); err != nil {
		return nil, err
	}
	var headers []*multipart.FileHeader
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		headers = r.MultipartForm.File[key]
	}
	if len(headers) == 0 {
		return nil, &Error{Source: SourceForm, Key: key, Kind: KindMissing, Err: http.ErrMissingFile}
	}
	return headers, nil
}

// parseMultipart parses the multipart form, using the configured in-memory cap
// (default 32 MiB). A non-multipart body maps to KindUnsupportedMediaType.
func parseMultipart(r *http.Request, opts []BodyOption) error {
	c := newBodyConfig(opts)
	mem := int64(defaultMultipartMemory)
	if c.maxBytesSet && c.maxBytes > 0 {
		mem = c.maxBytes
	}
	if err := r.ParseMultipartForm(mem); err != nil {
		kind := KindInvalidBody
		if errors.Is(err, http.ErrNotMultipart) {
			kind = KindUnsupportedMediaType
		}
		return &Error{Source: SourceBody, Kind: kind, Err: err}
	}
	return nil
}

// FileOption configures ValidateFile.
type FileOption func(*fileConfig)

type fileConfig struct {
	allowedMIME []string
	maxSize     int64
}

// WithAllowedMIME restricts uploads to these magic-byte-sniffed MIME types (e.g.
// "image/png", "application/pdf"). With no allowlist the MIME is not checked.
func WithAllowedMIME(mimes ...string) FileOption {
	return func(c *fileConfig) { c.allowedMIME = mimes }
}

// WithMaxFileSize rejects uploads whose declared size exceeds n bytes. With no
// limit the size is not checked.
func WithMaxFileSize(n int64) FileOption {
	return func(c *fileConfig) { c.maxSize = n }
}

// ValidateFile validates an uploaded file by its magic bytes (core/filetype),
// deliberately ignoring the client-declared Content-Type, plus an optional size
// cap. It returns a *Error: KindTooLarge for an oversize file,
// KindUnsupportedMediaType for a disallowed/undetectable type, KindMissing for a
// nil header; nil on success. Consumers with multiple files loop over Files().
func ValidateFile(fh *multipart.FileHeader, opts ...FileOption) error {
	if fh == nil {
		return &Error{Source: SourceForm, Kind: KindMissing}
	}
	var c fileConfig
	for _, o := range opts {
		o(&c)
	}
	if c.maxSize > 0 && fh.Size > c.maxSize {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindTooLarge}
	}
	if len(c.allowedMIME) == 0 {
		return nil
	}
	f, err := fh.Open()
	if err != nil {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType, Err: err}
	}
	defer func() { _ = f.Close() }()
	typ, _, err := filetype.DetectReader(f)
	if err != nil {
		return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType, Err: err}
	}
	if slices.Contains(c.allowedMIME, typ.MIME) {
		return nil
	}
	return &Error{Source: SourceForm, Key: fh.Filename, Kind: KindUnsupportedMediaType}
}
