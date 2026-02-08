package webhook_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/pkg/webhook"
)

func BenchmarkSender_Send(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := map[string]any{
		"event": "benchmark",
		"data":  map[string]string{"id": "123"},
	}

	b.ResetTimer()
	for b.Loop() {
		err := sender.Send(context.Background(), server.URL, payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSender_SendWithSignature(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := map[string]any{
		"event": "benchmark",
		"data":  map[string]string{"id": "123"},
	}

	b.ResetTimer()
	for b.Loop() {
		err := sender.Send(
			context.Background(),
			server.URL,
			payload,
			webhook.WithSignature("benchmark_secret"),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSender_HighThroughput_Sequential(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := map[string]any{
		"event": "high_throughput_test",
		"data": map[string]string{
			"id":        "bench_123",
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}

	b.ResetTimer()
	for b.Loop() {
		err := sender.Send(
			context.Background(),
			server.URL,
			payload,
			webhook.WithTimeout(5*time.Second),
			webhook.WithNoRetry(),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSender_HighThroughput_Concurrent(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := map[string]any{
		"event": "concurrent_test",
		"data": map[string]string{
			"id": "bench_456",
		},
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := sender.Send(
				context.Background(),
				server.URL,
				payload,
				webhook.WithTimeout(5*time.Second),
				webhook.WithNoRetry(),
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkSender_LargePayload(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()

	sizes := []struct {
		name string
		size int
	}{
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, sz := range sizes {
		b.Run(sz.name, func(b *testing.B) {
			data := make([]byte, sz.size)
			for i := range data {
				data[i] = byte(i % 256)
			}
			payload := map[string]any{
				"event": "large_payload_bench",
				"data":  data,
			}

			b.ResetTimer()
			for b.Loop() {
				err := sender.Send(
					context.Background(),
					server.URL,
					payload,
					webhook.WithTimeout(10*time.Second),
					webhook.WithNoRetry(),
					webhook.WithMaxPayloadSize(0),
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSender_WithRetries(b *testing.B) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		if count%2 == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	payload := map[string]string{"test": "retry_bench"}

	b.ResetTimer()
	for b.Loop() {
		err := sender.Send(
			context.Background(),
			server.URL,
			payload,
			webhook.WithMaxRetries(1),
			webhook.WithBackoff(webhook.FixedBackoff{Interval: time.Millisecond}),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSender_CircuitBreaker(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := webhook.NewCircuitBreaker(5, 2, 100*time.Millisecond)
	sender := webhook.NewSender()
	payload := map[string]string{"test": "circuit_bench"}

	b.ResetTimer()
	for b.Loop() {
		err := sender.Send(
			context.Background(),
			server.URL,
			payload,
			webhook.WithCircuitBreaker(cb),
			webhook.WithNoRetry(),
		)
		if err != nil {
			if !errors.Is(err, webhook.ErrCircuitOpen) {
				b.Fatal(err)
			}
		}
	}
}
