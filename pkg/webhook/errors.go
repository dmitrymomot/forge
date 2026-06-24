package webhook

import "errors"

// Domain errors for webhook operations, designed for error wrapping and classification.
//
// Error classification strategy:
//   - Configuration errors: Invalid setup or parameters (fail fast)
//   - Delivery errors: Network, timeout, or HTTP failures (may retry)
//   - Circuit breaker: Protection mechanism when endpoint consistently fails
var (
	ErrWebhookDeliveryFailed = errors.New("webhook: delivery failed")
	ErrInvalidConfiguration  = errors.New("webhook: invalid configuration")
	ErrPermanentFailure      = errors.New("webhook: permanent failure")
	ErrTemporaryFailure      = errors.New("webhook: temporary failure")
	ErrCircuitOpen           = errors.New("webhook: circuit breaker is open")
	ErrInvalidPayload        = errors.New("webhook: invalid payload")
	ErrInvalidURL            = errors.New("webhook: invalid URL")
	ErrTimeout               = errors.New("webhook: request timeout")
	ErrBlockedDestination    = errors.New("webhook: destination blocked by SSRF protection")
)
