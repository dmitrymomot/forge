package webhook

import "errors"

var (
	// ErrInvalidEndpoint means Send got an endpoint it must not deliver to: an
	// empty secret (unsigned webhooks are never sent) or a URL that is not
	// absolute http(s).
	ErrInvalidEndpoint = errors.New("webhook: invalid endpoint")

	// ErrTransientStatus means the receiver answered 408, 429, or 5xx —
	// worth retrying.
	ErrTransientStatus = errors.New("webhook: transient delivery status")

	// ErrPermanentStatus means the receiver answered a non-2xx status outside
	// the transient set (including redirects, which deliveries never follow) —
	// the endpoint or event is wrong; retrying won't help.
	ErrPermanentStatus = errors.New("webhook: permanent delivery status")

	// ErrEndpointNotFound is the Resolver contract: the stored endpoint no
	// longer exists (deleted or disabled), so the queued delivery is moot and
	// is cancelled rather than retried or dead-lettered.
	ErrEndpointNotFound = errors.New("webhook: endpoint not found")

	// ErrInvalidDelivery is returned by Enqueue on an empty endpoint ID or a
	// payload that is not valid JSON.
	ErrInvalidDelivery = errors.New("webhook: invalid delivery")

	// ErrMissingSignature means the request carries no signature (or, for
	// timestamped schemes, no timestamp) header.
	ErrMissingSignature = errors.New("webhook: missing signature")

	// ErrInvalidSignature means the signature does not authenticate the
	// payload under any accepted secret. The Verify middleware also collapses
	// secret-lookup failures to this sentinel — fail closed, no detail leaks.
	ErrInvalidSignature = errors.New("webhook: invalid signature")

	// ErrInvalidTimestamp means a timestamped scheme got an unparseable
	// timestamp or one outside the verification tolerance window.
	ErrInvalidTimestamp = errors.New("webhook: invalid or expired timestamp")

	// ErrBodyTooLarge means the inbound request body exceeds the Verify
	// middleware's cap (default 1 MiB, WithMaxBody).
	ErrBodyTooLarge = errors.New("webhook: request body exceeds limit")

	// ErrUnreadableBody means the inbound request body could not be read
	// (client hung up mid-request).
	ErrUnreadableBody = errors.New("webhook: unreadable request body")
)
