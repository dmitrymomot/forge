// Package errorsx adds two orthogonal, stack-free capabilities to stdlib errors:
// a string CODE for mapping an internal error to an HTTP/API status
// (New/Errorf/WithCode/Code), and a permanent/retryable tag for controlling
// retry behavior (MarkPermanent/IsPermanent/IsRetryable). Both are unexported
// wrapper types that compose with errors.Is/As/Unwrap, so a code or tag is found
// anywhere in the chain and wrapped sentinels keep matching.
//
// errorsx is NOT a stack-trace or error-reporting library (it captures no
// stacks and renders single-line errors), and it does NOT map codes to HTTP
// statuses itself — that belongs to the future problem/render layer, which reads
// Code. For plain wrapping use fmt.Errorf with %w; reach for errorsx only when
// you need a code or a retry tag.
package errorsx
