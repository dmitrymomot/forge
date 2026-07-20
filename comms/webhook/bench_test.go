package webhook_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/comms/webhook"
)

var benchPayload = []byte(`{"type":"invoice.paid","id":"evt_1","data":{"object":{"id":"in_1","amount_due":1250,"currency":"usd","customer":"cus_9","status":"paid"}}}`)

func BenchmarkSchemeSign(b *testing.B) {
	now := time.Now()
	for name, scheme := range map[string]webhook.Scheme{
		"stripe": webhook.Stripe(),
		"github": webhook.GitHub(),
		"slack":  webhook.Slack(),
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := scheme.Sign(testSecret, benchPayload, now); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSchemeVerify(b *testing.B) {
	now := time.Now()
	for name, scheme := range map[string]webhook.Scheme{
		"stripe": webhook.Stripe(),
		"github": webhook.GitHub(),
		"slack":  webhook.Slack(),
	} {
		b.Run(name, func(b *testing.B) {
			h, err := scheme.Sign(testSecret, benchPayload, now)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := scheme.Verify(testSecret, benchPayload, h, now, 5*time.Minute); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkVerifyMiddleware(b *testing.B) {
	scheme := webhook.Stripe()
	handler := webhook.Verify(scheme, webhook.StaticSecrets(testSecret))(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
		}),
	)
	sig, err := scheme.Sign(testSecret, benchPayload, time.Now())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(benchPayload))
		req.Header = sig.Clone()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
