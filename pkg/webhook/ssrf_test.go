package webhook_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/webhook"
)

// TestSender_Send_SSRF_BlocksPrivateByDefault verifies that, without an explicit
// opt-out, the sender refuses to deliver to non-public destinations. This is the
// trust boundary: user-supplied webhook URLs cannot be used to reach internal
// infrastructure.
func TestSender_Send_SSRF_BlocksPrivateByDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"loopback IPv4", "http://127.0.0.1:8080/hook"},
		{"loopback IPv6", "http://[::1]:8080/hook"},
		{"private 10.x", "http://10.0.0.5/hook"},
		{"private 192.168.x", "http://192.168.1.10/hook"},
		{"private 172.16.x", "http://172.16.0.1/hook"},
		{"link-local 169.254.x", "http://169.254.169.254/latest/meta-data"},
		{"unspecified 0.0.0.0", "http://0.0.0.0/hook"},
		{"unique-local IPv6", "http://[fd00::1]/hook"},
	}

	sender := webhook.NewSender()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := sender.Send(context.Background(), tt.url, map[string]string{"k": "v"})
			require.Error(t, err)
			require.ErrorIs(t, err, webhook.ErrBlockedDestination,
				"private/loopback destination %s must be blocked by default", tt.url)
		})
	}
}

// TestSender_Send_SSRF_AllowsPrivateWithOptOut verifies the opt-out: a caller that
// explicitly trusts internal endpoints can deliver to a loopback test server.
func TestSender_Send_SSRF_AllowsPrivateWithOptOut(t *testing.T) {
	t.Parallel()

	var hit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewSender()
	err := sender.Send(
		context.Background(),
		server.URL, // 127.0.0.1
		map[string]string{"k": "v"},
		webhook.WithAllowPrivateNetworks(),
	)
	require.NoError(t, err)
	require.True(t, hit, "request must reach the loopback server when opted in")
}
