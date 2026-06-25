package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/webhook"
)

func TestSender_Send_Success(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"event": "test",
		"id":    "123",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "forge-webhook/1.0", r.Header.Get("User-Agent"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var received map[string]any
		err = json.Unmarshal(body, &received)
		require.NoError(t, err)
		assert.Equal(t, payload, received)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), server.URL, payload, webhook.WithAllowPrivateNetworks())
	require.NoError(t, err)
}

func TestSender_Send_WithOptions(t *testing.T) {
	t.Parallel()

	payload := map[string]string{"test": "data"}
	secret := "webhook_secret"

	var deliveryResults []webhook.DeliveryResult

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-value", r.Header.Get("X-Custom-Header"))

		assert.NotEmpty(t, r.Header.Get("X-Webhook-Signature"))
		assert.NotEmpty(t, r.Header.Get("X-Webhook-Timestamp"))
		assert.NotEmpty(t, r.Header.Get("X-Webhook-ID"))

		headers, err := webhook.ExtractSignatureHeaders(map[string]string{
			"X-Webhook-Signature": r.Header.Get("X-Webhook-Signature"),
			"X-Webhook-Timestamp": r.Header.Get("X-Webhook-Timestamp"),
			"X-Webhook-ID":        r.Header.Get("X-Webhook-ID"),
		})
		require.NoError(t, err)

		body, _ := io.ReadAll(r.Body)
		err = webhook.VerifySignature(secret, body, headers, 5*time.Minute)
		require.NoError(t, err)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(
		context.Background(),
		server.URL,
		payload,
		webhook.WithSignature(secret),
		webhook.WithHeader("X-Custom-Header", "test-value"),
		webhook.WithTimeout(5*time.Second),
		webhook.WithMaxRetries(2),
		webhook.WithAllowPrivateNetworks(),
		webhook.WithOnDelivery(func(result webhook.DeliveryResult) {
			deliveryResults = append(deliveryResults, result)
		}),
	)

	require.NoError(t, err)
	require.Len(t, deliveryResults, 1)
	assert.True(t, deliveryResults[0].Success)
	assert.Equal(t, http.StatusOK, deliveryResults[0].StatusCode)
}

func TestSender_Send_Retries(t *testing.T) {
	t.Parallel()

	payload := map[string]string{"test": "retry"}
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)

		if attempt < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("temporary error"))
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(
		context.Background(),
		server.URL,
		payload,
		webhook.WithMaxRetries(3),
		webhook.WithBackoff(webhook.FixedBackoff{Interval: 10 * time.Millisecond}),
		webhook.WithAllowPrivateNetworks(),
	)

	require.NoError(t, err)
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

func TestSender_Send_PermanentFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statusCode  int
		shouldRetry bool
	}{
		{"400 Bad Request", http.StatusBadRequest, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"403 Forbidden", http.StatusForbidden, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"408 Request Timeout", http.StatusRequestTimeout, true},
		{"425 Too Early", http.StatusTooEarly, true},
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&attempts, 1)
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte("error message"))
			}))
			defer server.Close()

			sender := webhook.NewSender()
			err := sender.Send(
				context.Background(),
				server.URL,
				map[string]string{"test": "data"},
				webhook.WithMaxRetries(3),
				webhook.WithBackoff(webhook.FixedBackoff{Interval: time.Millisecond}),
				webhook.WithAllowPrivateNetworks(),
			)

			require.Error(t, err)

			if tt.shouldRetry {
				assert.Equal(t, int32(4), atomic.LoadInt32(&attempts), "should retry for %d", tt.statusCode)
				require.ErrorIs(t, err, webhook.ErrWebhookDeliveryFailed)
			} else {
				assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "should not retry for %d", tt.statusCode)
				require.ErrorIs(t, err, webhook.ErrPermanentFailure)
			}
		})
	}
}

func TestSender_Send_CircuitBreaker(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cb := webhook.NewCircuitBreaker(2, 1, 100*time.Millisecond)
	sender := webhook.NewSender()

	for range 2 {
		err := sender.Send(
			context.Background(),
			server.URL,
			map[string]string{"test": "data"},
			webhook.WithCircuitBreaker(cb),
			webhook.WithNoRetry(),
			webhook.WithAllowPrivateNetworks(),
		)
		require.Error(t, err)
	}

	assert.Equal(t, webhook.CircuitOpen, cb.State())

	err := sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "data"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithAllowPrivateNetworks(),
	)
	require.ErrorIs(t, err, webhook.ErrCircuitOpen)

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, webhook.CircuitHalfOpen, cb.State())
}

func TestSender_Send_CircuitBreaker_ConsultedPerAttempt(t *testing.T) {
	t.Parallel()

	// Server always fails so every attempt records a circuit-breaker failure.
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// failureThreshold=2 means the breaker opens after the 2nd failure. With the
	// breaker consulted on every attempt, the 3rd attempt is short-circuited
	// before the request is sent, so the server sees exactly 2 hits even though
	// maxRetries permits up to 6 total attempts.
	cb := webhook.NewCircuitBreaker(2, 1, time.Hour)
	sender := webhook.NewSender()

	err := sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "data"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithMaxRetries(5),
		webhook.WithBackoff(webhook.FixedBackoff{Interval: time.Millisecond}),
		webhook.WithAllowPrivateNetworks(),
	)

	require.Error(t, err)
	require.ErrorIs(t, err, webhook.ErrCircuitOpen,
		"once the breaker trips mid-retry, remaining attempts must short-circuit")
	require.Equal(t, int32(2), atomic.LoadInt32(&attempts),
		"breaker should stop further attempts after it opens")
	require.Equal(t, webhook.CircuitOpen, cb.State())
}

func TestSender_Send_NegativeRetriesClamped(t *testing.T) {
	t.Parallel()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := webhook.NewSender()

	// Negative attempt counts must be clamped to 0 (no retries), so the server
	// is hit exactly once regardless of the negative value supplied.
	for _, opt := range []webhook.SendOption{
		webhook.WithBasicRetry(-3, time.Millisecond),
		webhook.WithExponentialRetry(-3, time.Millisecond, time.Second),
	} {
		atomic.StoreInt32(&attempts, 0)
		err := sender.Send(
			context.Background(),
			server.URL,
			map[string]string{"test": "data"},
			opt,
			webhook.WithAllowPrivateNetworks(),
		)
		require.Error(t, err)
		require.Equal(t, int32(1), atomic.LoadInt32(&attempts),
			"negative retry count must be clamped to a single attempt")
	}
}

func TestSender_Send_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "data"},
		webhook.WithTimeout(50*time.Millisecond),
		webhook.WithNoRetry(),
		webhook.WithAllowPrivateNetworks(),
	)

	require.Error(t, err)
	require.ErrorIs(t, err, webhook.ErrTimeout)
}

func TestSender_Send_ContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	sender := webhook.NewSender()
	err := sender.Send(ctx, server.URL, map[string]string{"test": "data"}, webhook.WithAllowPrivateNetworks())

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestSender_Send_ValidationErrors(t *testing.T) {
	t.Parallel()

	sender := webhook.NewSender()

	tests := []struct {
		name    string
		url     string
		payload any
		wantErr error
		errMsg  string
	}{
		{
			name:    "empty URL",
			url:     "",
			payload: map[string]string{"test": "data"},
			wantErr: webhook.ErrInvalidURL,
			errMsg:  "URL is required",
		},
		{
			name:    "invalid URL",
			url:     "not a url",
			payload: map[string]string{"test": "data"},
			wantErr: webhook.ErrInvalidURL,
			errMsg:  "only http and https schemes are supported",
		},
		{
			name:    "invalid scheme",
			url:     "ftp://example.com",
			payload: map[string]string{"test": "data"},
			wantErr: webhook.ErrInvalidURL,
			errMsg:  "only http and https schemes are supported",
		},
		{
			name:    "missing host",
			url:     "http:///path",
			payload: map[string]string{"test": "data"},
			wantErr: webhook.ErrInvalidURL,
			errMsg:  "host is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sender.Send(context.Background(), tt.url, tt.payload)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestSender_Send_DeliveryHook(t *testing.T) {
	t.Parallel()

	var results []webhook.DeliveryResult
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "data"},
		webhook.WithMaxRetries(2),
		webhook.WithBackoff(webhook.FixedBackoff{Interval: time.Millisecond}),
		webhook.WithAllowPrivateNetworks(),
		webhook.WithOnDelivery(func(result webhook.DeliveryResult) {
			results = append(results, result)
		}),
	)

	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.False(t, results[0].Success)
	assert.Equal(t, http.StatusInternalServerError, results[0].StatusCode)
	assert.Equal(t, 1, results[0].Attempt)
	assert.NotNil(t, results[0].Error)

	assert.True(t, results[1].Success)
	assert.Equal(t, http.StatusOK, results[1].StatusCode)
	assert.Equal(t, 2, results[1].Attempt)
	assert.Nil(t, results[1].Error)
}

func TestSender_Send_LargePayload(t *testing.T) {
	t.Parallel()

	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := map[string]any{
		"data": largeData,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var decoded map[string]any
		err = json.Unmarshal(body, &decoded)
		require.NoError(t, err)

		_, ok := decoded["data"]
		assert.True(t, ok)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), server.URL, payload, webhook.WithAllowPrivateNetworks())
	require.NoError(t, err)
}

func TestSender_Concurrent(t *testing.T) {
	t.Parallel()

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()

	errCh := make(chan error, 10)
	for i := range 10 {
		go func(id int) {
			payload := map[string]int{"id": id}
			err := sender.Send(context.Background(), server.URL, payload, webhook.WithAllowPrivateNetworks())
			errCh <- err
		}(i)
	}

	for range 10 {
		err := <-errCh
		require.NoError(t, err)
	}

	assert.Equal(t, int32(10), atomic.LoadInt32(&requests))
}

func TestSender_CircuitBreaker_HalfOpenRecovery(t *testing.T) {
	t.Parallel()

	var attempts int32
	var succeedAfter int32 = 3

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)
		if attempt <= succeedAfter {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := webhook.NewCircuitBreaker(2, 2, 100*time.Millisecond)
	sender := webhook.NewSender()

	for range 2 {
		err := sender.Send(
			context.Background(),
			server.URL,
			map[string]string{"test": "failure"},
			webhook.WithCircuitBreaker(cb),
			webhook.WithNoRetry(),
			webhook.WithAllowPrivateNetworks(),
		)
		require.Error(t, err)
	}

	assert.Equal(t, webhook.CircuitOpen, cb.State())

	time.Sleep(150 * time.Millisecond)

	err := sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "halfopen_fail"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithNoRetry(),
		webhook.WithAllowPrivateNetworks(),
	)
	require.Error(t, err)
	assert.Equal(t, webhook.CircuitOpen, cb.State())

	time.Sleep(150 * time.Millisecond)

	err = sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "success1"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithNoRetry(),
		webhook.WithAllowPrivateNetworks(),
	)
	require.NoError(t, err)
	assert.Equal(t, webhook.CircuitHalfOpen, cb.State())

	err = sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "success2"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithNoRetry(),
		webhook.WithAllowPrivateNetworks(),
	)
	require.NoError(t, err)
	assert.Equal(t, webhook.CircuitClosed, cb.State())

	err = sender.Send(
		context.Background(),
		server.URL,
		map[string]string{"test": "verify_closed"},
		webhook.WithCircuitBreaker(cb),
		webhook.WithNoRetry(),
		webhook.WithAllowPrivateNetworks(),
	)
	require.NoError(t, err)
	assert.Equal(t, webhook.CircuitClosed, cb.State())
}

func TestSender_CircuitBreaker_Concurrent(t *testing.T) {
	t.Parallel()

	var requests int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)

		if atomic.LoadInt32(&requests)%3 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := webhook.NewCircuitBreaker(5, 2, 50*time.Millisecond)
	sender := webhook.NewSender()

	const numGoroutines = 20
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var totalErrors int32
	var circuitOpenErrors int32

	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			for j := range requestsPerGoroutine {
				payload := map[string]int{"goroutine": id, "request": j}
				err := sender.Send(
					context.Background(),
					server.URL,
					payload,
					webhook.WithCircuitBreaker(cb),
					webhook.WithNoRetry(),
					webhook.WithTimeout(100*time.Millisecond),
					webhook.WithAllowPrivateNetworks(),
				)

				if err != nil {
					atomic.AddInt32(&totalErrors, 1)
					if errors.Is(err, webhook.ErrCircuitOpen) {
						atomic.AddInt32(&circuitOpenErrors, 1)
					}
				}

				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	finalState := cb.State()
	assert.Contains(t, []webhook.CircuitState{
		webhook.CircuitClosed,
		webhook.CircuitOpen,
		webhook.CircuitHalfOpen,
	}, finalState)
}

func TestSender_Send_MarshalError(t *testing.T) {
	t.Parallel()

	type UnmarshalableType struct {
		Ch chan int `json:"channel"`
	}

	data := UnmarshalableType{
		Ch: make(chan int),
	}

	sender := webhook.NewSender()
	err := sender.Send(context.Background(), "https://example.com", data)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal payload to JSON")
}

func TestSender_Send_PayloadSizeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		payloadSize    int
		maxPayloadSize int64
		expectError    bool
		errorContains  string
	}{
		{
			name:           "payload within limit",
			payloadSize:    1024,
			maxPayloadSize: 10 * 1024,
			expectError:    false,
		},
		{
			name:           "payload exactly at limit",
			payloadSize:    700,
			maxPayloadSize: 1024,
			expectError:    false,
		},
		{
			name:           "payload exceeds limit",
			payloadSize:    2 * 1024,
			maxPayloadSize: 1024,
			expectError:    true,
			errorContains:  "exceeds maximum allowed size",
		},
		{
			name:           "no limit when set to 0",
			payloadSize:    10 * 1024 * 1024,
			maxPayloadSize: 0,
			expectError:    false,
		},
		{
			name:           "default 10MB limit",
			payloadSize:    11 * 1024 * 1024,
			maxPayloadSize: -1,
			expectError:    true,
			errorContains:  "exceeds maximum allowed size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := make([]byte, tt.payloadSize)
			for i := range data {
				data[i] = byte(i % 256)
			}
			payload := map[string]any{
				"data": data,
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)

				var decoded map[string]any
				err = json.Unmarshal(body, &decoded)
				require.NoError(t, err)

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			sender := webhook.NewSender()

			opts := []webhook.SendOption{webhook.WithAllowPrivateNetworks()}
			if tt.maxPayloadSize >= 0 {
				opts = append(opts, webhook.WithMaxPayloadSize(tt.maxPayloadSize))
			}

			err := sender.Send(context.Background(), server.URL, payload, opts...)

			if tt.expectError {
				require.Error(t, err)
				require.ErrorIs(t, err, webhook.ErrInvalidPayload)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSender_Send_ResponseSizeLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		responseSize    int
		maxResponseSize int64
		statusCode      int
	}{
		{
			name:            "small response within limit",
			responseSize:    1024,
			maxResponseSize: 64 * 1024,
			statusCode:      http.StatusBadRequest,
		},
		{
			name:            "large response truncated",
			responseSize:    100 * 1024,
			maxResponseSize: 10 * 1024,
			statusCode:      http.StatusInternalServerError,
		},
		{
			name:            "custom response limit",
			responseSize:    5 * 1024,
			maxResponseSize: 2 * 1024,
			statusCode:      http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			responseBody := make([]byte, tt.responseSize)
			for i := range responseBody {
				responseBody[i] = byte('A' + (i % 26))
			}

			var capturedErrorMsg string

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write(responseBody)
			}))
			defer server.Close()

			sender := webhook.NewSender()
			err := sender.Send(
				context.Background(),
				server.URL,
				map[string]string{"test": "data"},
				webhook.WithMaxResponseSize(tt.maxResponseSize),
				webhook.WithNoRetry(),
				webhook.WithAllowPrivateNetworks(),
				webhook.WithOnDelivery(func(result webhook.DeliveryResult) {
					if result.Error != nil {
						capturedErrorMsg = result.Error.Error()
					}
				}),
			)

			require.Error(t, err)
			assert.NotEmpty(t, capturedErrorMsg)

			parts := strings.SplitN(capturedErrorMsg, ": ", 2)
			if len(parts) == 2 {
				bodyInError := parts[1]
				maxExpectedLen := min(int(tt.maxResponseSize), 200) + 10
				assert.LessOrEqual(t, len(bodyInError), maxExpectedLen,
					"Error message body portion should be limited by maxResponseSize")
			}
		})
	}
}

func TestSender_CircuitBreaker_WithLargePayload(t *testing.T) {
	t.Parallel()

	largeData := make([]byte, 100*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	payload := map[string]any{
		"event": "large_payload_test",
		"data":  largeData,
	}

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&attempts, 1)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var decoded map[string]any
		err = json.Unmarshal(body, &decoded)
		require.NoError(t, err)

		if attempt <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := webhook.NewCircuitBreaker(2, 1, 100*time.Millisecond)
	sender := webhook.NewSender()

	for range 2 {
		err := sender.Send(
			context.Background(),
			server.URL,
			payload,
			webhook.WithCircuitBreaker(cb),
			webhook.WithNoRetry(),
			webhook.WithTimeout(5*time.Second),
			webhook.WithAllowPrivateNetworks(),
		)
		require.Error(t, err)
		require.ErrorIs(t, err, webhook.ErrWebhookDeliveryFailed)
	}

	assert.Equal(t, webhook.CircuitOpen, cb.State())

	err := sender.Send(
		context.Background(),
		server.URL,
		payload,
		webhook.WithCircuitBreaker(cb),
		webhook.WithTimeout(5*time.Second),
		webhook.WithAllowPrivateNetworks(),
	)
	require.ErrorIs(t, err, webhook.ErrCircuitOpen)

	time.Sleep(150 * time.Millisecond)

	err = sender.Send(
		context.Background(),
		server.URL,
		payload,
		webhook.WithCircuitBreaker(cb),
		webhook.WithNoRetry(),
		webhook.WithTimeout(5*time.Second),
		webhook.WithAllowPrivateNetworks(),
	)
	require.NoError(t, err)
	assert.Equal(t, webhook.CircuitClosed, cb.State())

	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}
