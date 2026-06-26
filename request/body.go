package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
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

	if !c.skipCType && !matchesMediaType(r.Header.Get("Content-Type"), "application/json") {
		return &Error{
			Source: SourceBody,
			Kind:   KindUnsupportedMediaType,
			Err:    fmt.Errorf("content-type %q is not application/json", r.Header.Get("Content-Type")),
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
