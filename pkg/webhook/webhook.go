package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sender provides reliable webhook delivery with retries and circuit breaking.
// Zero value is not usable; use NewSender to create instances.
type Sender struct {
	client *http.Client
}

// NewSender creates a webhook sender with default HTTP client.
// Connection pooling is configured for high-throughput scenarios.
func NewSender() *Sender {
	return &Sender{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// NewSenderWithClient creates a webhook sender with a custom HTTP client.
// This allows for custom transports, proxies, or testing.
func NewSenderWithClient(client *http.Client) *Sender {
	if client == nil {
		return NewSender()
	}
	return &Sender{client: client}
}

// Send delivers a webhook payload to the specified URL with retry logic.
// The payload is marshaled to JSON and sent as a POST request.
// Options control timeout, retries, signing, and other behavior.
func (s *Sender) Send(ctx context.Context, webhookURL string, data any, opts ...SendOption) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal payload to JSON: %w", err)
	}

	if err := s.validateInputs(webhookURL, payload); err != nil {
		return err
	}

	options := defaultSendOptions()
	for _, opt := range opts {
		opt(options)
	}

	if options.maxPayloadSize > 0 && int64(len(payload)) > options.maxPayloadSize {
		return fmt.Errorf("%w: payload size %d bytes exceeds maximum allowed size of %d bytes",
			ErrInvalidPayload, len(payload), options.maxPayloadSize)
	}

	client := s.client
	if options.httpClient != nil {
		client = options.httpClient
	}

	if options.circuitBreaker != nil && !options.circuitBreaker.Allow() {
		return ErrCircuitOpen
	}

	var lastErr error
	for attempt := 0; attempt <= options.maxRetries; attempt++ {
		if attempt > 0 {
			delay := options.backoffStrategy.NextInterval(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		result, err := s.attemptDelivery(ctx, client, webhookURL, payload, options)

		if options.onDelivery != nil {
			result.Attempt = attempt + 1
			options.onDelivery(result)
		}

		if options.circuitBreaker != nil {
			if err == nil {
				options.circuitBreaker.RecordSuccess()
			} else {
				options.circuitBreaker.RecordFailure()
			}
		}

		if err == nil {
			return nil
		}

		lastErr = err

		if isPermanentError(result.StatusCode, err) {
			return fmt.Errorf("%w: %w", ErrPermanentFailure, err)
		}
	}

	return fmt.Errorf("%w after %d attempts: %w", ErrWebhookDeliveryFailed, options.maxRetries+1, lastErr)
}

func (s *Sender) validateInputs(webhookURL string, payload []byte) error {
	if webhookURL == "" {
		return fmt.Errorf("%w: URL is required", ErrInvalidURL)
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: only http and https schemes are supported", ErrInvalidURL)
	}

	if u.Host == "" {
		return fmt.Errorf("%w: host is required", ErrInvalidURL)
	}

	if len(payload) == 0 {
		return fmt.Errorf("%w: payload cannot be empty", ErrInvalidPayload)
	}

	return nil
}

func (s *Sender) attemptDelivery(ctx context.Context, client *http.Client, webhookURL string, payload []byte, options *sendOptions) (DeliveryResult, error) {
	start := time.Now()
	result := DeliveryResult{}

	reqCtx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		result.Duration = time.Since(start)
		result.Error = err
		return result, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "forge-webhook/1.0")

	for k, v := range options.headers {
		req.Header.Set(k, v)
	}

	if options.signatureSecret != "" {
		sigHeaders, err := SignPayload(options.signatureSecret, payload)
		if err != nil {
			result.Duration = time.Since(start)
			result.Error = err
			return result, fmt.Errorf("failed to sign payload: %w", err)
		}
		for k, v := range sigHeaders.Headers() {
			req.Header.Set(k, v)
		}
	}

	resp, err := client.Do(req)
	result.Duration = time.Since(start)

	if err != nil {
		result.Error = err
		if reqCtx.Err() == context.DeadlineExceeded {
			return result, fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		return result, fmt.Errorf("%w: %w", ErrTemporaryFailure, err)
	}
	if resp == nil {
		return result, fmt.Errorf("%w: nil response", ErrTemporaryFailure)
	}

	defer func() { _ = resp.Body.Close() }()
	result.StatusCode = resp.StatusCode
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300

	body, _ := io.ReadAll(io.LimitReader(resp.Body, options.maxResponseSize))

	if !result.Success {
		errMsg := fmt.Sprintf("webhook returned status %d", resp.StatusCode)
		if len(body) > 0 {
			bodyStr := strings.ReplaceAll(string(body), "\n", " ")
			if len(bodyStr) > 200 {
				bodyStr = bodyStr[:200] + "..."
			}
			errMsg += fmt.Sprintf(": %s", bodyStr)
		}
		result.Error = fmt.Errorf("%s", errMsg)
		return result, result.Error
	}

	return result, nil
}

// isPermanentError determines if an error should not be retried.
// Most 4xx errors indicate client-side issues that won't resolve with retries,
// but some 4xx codes represent temporary server-side issues.
func isPermanentError(statusCode int, _ error) bool {
	if statusCode >= 400 && statusCode < 500 {
		switch statusCode {
		case 408: // Request Timeout
			return false
		case 425: // Too Early
			return false
		case 429: // Too Many Requests
			return false
		default:
			return true
		}
	}
	return false
}
