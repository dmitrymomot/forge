package binder

import "errors"

// Error variables define common binding failures that can occur during request processing.
var (
	// ErrUnsupportedMediaType indicates the Content-Type header specifies a media type
	// that the binder doesn't support (e.g., text/plain for JSON binder).
	ErrUnsupportedMediaType = errors.New("unsupported media type")

	// ErrFailedToParseJSON indicates the request body contains invalid JSON
	// or doesn't match the target struct schema.
	ErrFailedToParseJSON = errors.New("failed to parse JSON request body")

	// ErrFailedToParseForm indicates form data parsing failed due to malformed
	// multipart boundaries or invalid URL-encoded data.
	ErrFailedToParseForm = errors.New("failed to parse form data")

	// ErrFailedToParseQuery indicates query parameter parsing failed,
	// typically due to type conversion errors.
	ErrFailedToParseQuery = errors.New("failed to parse query parameters")

	// ErrFailedToParsePath indicates path parameter extraction or conversion failed.
	ErrFailedToParsePath = errors.New("failed to parse path parameters")

	// ErrMissingContentType indicates the request lacks a Content-Type header
	// when one is required for parsing.
	ErrMissingContentType = errors.New("missing content type")

	// ErrBinderNotApplicable indicates the binder cannot process the request
	// (e.g., wrong HTTP method or missing required data).
	ErrBinderNotApplicable = errors.New("binder not applicable for this request")
)
