package webhook

import (
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestValidateDestination exercises the SSRF guard's resolution logic directly
// with an injected resolver so the hostname path (name -> IP) is covered
// deterministically without real DNS.
func TestValidateDestination(t *testing.T) {
	t.Parallel()

	resolve := func(ips ...string) func(string) ([]net.IP, error) {
		return func(string) ([]net.IP, error) {
			out := make([]net.IP, 0, len(ips))
			for _, s := range ips {
				out = append(out, net.ParseIP(s))
			}
			return out, nil
		}
	}

	t.Run("public hostname is allowed", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://hooks.example.com/x", false, resolve("93.184.216.34"))
		require.NoError(t, err)
	})

	t.Run("hostname resolving to private IP is blocked", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://internal.example.com/x", false, resolve("10.1.2.3"))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
	})

	t.Run("hostname resolving to loopback is blocked", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://rebind.example.com/x", false, resolve("127.0.0.1"))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
	})

	t.Run("any private result in a multi-IP set blocks", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://mixed.example.com/x", false, resolve("93.184.216.34", "192.168.0.9"))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
	})

	t.Run("public IP literal is allowed", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://8.8.8.8/x", false, failingResolver(t))
		require.NoError(t, err)
	})

	t.Run("private IP literal is blocked without DNS", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://10.0.0.1/x", false, failingResolver(t))
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
	})

	t.Run("opt-out skips resolution entirely", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://internal.example.com/x", true, failingResolver(t))
		require.NoError(t, err)
	})

	t.Run("resolution failure is reported as blocked", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("dns boom")
		err := validateDestination("https://broken.example.com/x", false, func(string) ([]net.IP, error) {
			return nil, boom
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
		require.ErrorIs(t, err, boom)
	})

	t.Run("empty resolution is blocked", func(t *testing.T) {
		t.Parallel()
		err := validateDestination("https://empty.example.com/x", false, func(string) ([]net.IP, error) {
			return nil, nil
		})
		require.Error(t, err)
		require.ErrorIs(t, err, ErrBlockedDestination)
	})
}

// failingResolver returns a resolver that fails the test if invoked, proving the
// IP-literal and opt-out paths never trigger DNS.
func failingResolver(t *testing.T) func(string) ([]net.IP, error) {
	t.Helper()
	return func(host string) ([]net.IP, error) {
		t.Errorf("resolver should not be called for %q", host)
		return nil, nil
	}
}
