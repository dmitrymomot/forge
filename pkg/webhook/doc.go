// Package webhook provides reliable HTTP webhook delivery with automatic retries and circuit breaking.
//
// The package handles JSON marshaling, HMAC signatures, exponential backoff, and connection pooling
// to deliver webhooks reliably to remote endpoints. It's designed for production use with proper
// error handling, timeout management, and observability hooks.
//
// # Basic Usage
//
// Send a webhook with default settings:
//
//	sender := webhook.NewSender()
//
//	event := map[string]any{
//		"type":      "user.created",
//		"user_id":   "123",
//		"timestamp": time.Now().Unix(),
//	}
//
//	ctx := context.Background()
//	err := sender.Send(ctx, "https://api.example.com/webhooks", event)
//	if err != nil {
//		log.Printf("Webhook delivery failed: %v", err)
//	}
//
// # Configuration Options
//
// Configure timeout, retries, and signatures:
//
//	err := sender.Send(ctx, webhookURL, event,
//		webhook.WithTimeout(30*time.Second),
//		webhook.WithMaxRetries(3),
//		webhook.WithSignature("your-webhook-secret"),
//	)
//
// # Backoff Strategies
//
// Use custom backoff strategy:
//
//	backoff := webhook.ExponentialBackoff{
//		InitialInterval: 500 * time.Millisecond,
//		MaxInterval:     30 * time.Second,
//		Multiplier:      2.0,
//		JitterFactor:    0.1,
//	}
//
//	err := sender.Send(ctx, webhookURL, event,
//		webhook.WithBackoff(backoff),
//	)
//
// # Circuit Breaker
//
// Protect failing endpoints:
//
//	cb := webhook.NewCircuitBreaker(5, 2, 30*time.Second)
//
//	err := sender.Send(ctx, webhookURL, event,
//		webhook.WithCircuitBreaker(cb),
//	)
//
//	if errors.Is(err, webhook.ErrCircuitOpen) {
//		log.Println("Circuit breaker protecting endpoint")
//	}
//
// # Signature Verification
//
// Generate and verify signatures:
//
//	// Generate signature
//	payload, _ := json.Marshal(event)
//	sigHeaders, err := webhook.SignPayload("secret", payload)
//	if err != nil {
//		return err
//	}
//
//	// Extract from HTTP headers
//	headerMap := map[string]string{
//		"X-Webhook-Signature": r.Header.Get("X-Webhook-Signature"),
//		"X-Webhook-Timestamp": r.Header.Get("X-Webhook-Timestamp"),
//	}
//	extractedSig, err := webhook.ExtractSignatureHeaders(headerMap)
//
//	// Verify signature with 5 minute tolerance
//	err = webhook.VerifySignature("secret", payload, extractedSig, 5*time.Minute)
//	if err != nil {
//		http.Error(w, "Invalid signature", http.StatusUnauthorized)
//		return
//	}
//
// # SSRF Protection and Trust Boundary
//
// Webhook destination URLs are frequently user-supplied (for example, tenants
// configuring their own endpoints). An attacker-controlled URL can be used for
// Server-Side Request Forgery (SSRF) — reaching cloud metadata services,
// internal databases, or other infrastructure that is only reachable from
// inside your network.
//
// To make the safe behavior the default, the sender resolves each destination
// host before delivery and rejects any address that is loopback, private
// (RFC 1918 / RFC 4193 unique-local), link-local, or unspecified. Hostnames are
// resolved via DNS so names that point at internal addresses are caught too.
// Blocked destinations fail with ErrBlockedDestination:
//
//	err := sender.Send(ctx, userSuppliedURL, event)
//	if errors.Is(err, webhook.ErrBlockedDestination) {
//		// URL points at a non-public address; reject it.
//	}
//
// Callers that legitimately deliver to internal endpoints (a service inside
// their own network, or a local test server) can opt out with
// WithAllowPrivateNetworks. Only enable this for trusted destinations:
//
//	err := sender.Send(ctx, "http://internal-service.local/hook", event,
//		webhook.WithAllowPrivateNetworks(),
//	)
//
// # Monitoring
//
// Track delivery attempts:
//
//	err := sender.Send(ctx, webhookURL, event,
//		webhook.WithOnDelivery(func(result webhook.DeliveryResult) {
//			log.Printf("Attempt %d: success=%t, status=%d, duration=%v",
//				result.Attempt, result.Success, result.StatusCode, result.Duration)
//		}),
//	)
//
// # Error Types
//
// The package defines specific error types for different failure modes:
//   - ErrInvalidURL: URL is malformed or uses unsupported scheme
//   - ErrBlockedDestination: URL resolves to a non-public address (SSRF protection)
//   - ErrInvalidPayload: Payload is empty or exceeds size limit
//   - ErrTimeout: Request exceeded timeout
//   - ErrCircuitOpen: Circuit breaker is protecting the endpoint
//   - ErrPermanentFailure: 4xx HTTP status (won't retry)
//   - ErrTemporaryFailure: Network or 5xx error (will retry)
//   - ErrWebhookDeliveryFailed: All retry attempts exhausted
//   - ErrInvalidConfiguration: Invalid setup or parameters
//
// # Core Types
//
// Main types provided by this package:
//   - Sender: Main type for sending webhooks with retries and circuit breaking
//   - CircuitBreaker: Prevents hammering failing endpoints
//   - DeliveryResult: Information about a single delivery attempt
//   - SignatureHeaders: HMAC-SHA256 signature data for webhook authentication
//   - BackoffStrategy: Interface for custom retry delay strategies (ExponentialBackoff, LinearBackoff, FixedBackoff)
package webhook
