package httpserver

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseConfig() config {
	return config{Config: DefaultConfig(), logger: slog.Default()}
}

func TestDataOptions_SetFields(t *testing.T) {
	c := baseConfig()
	WithAddr(":9090")(&c)
	WithName("api")(&c)
	WithConfig(Config{Addr: ":1", ReadTimeout: 1})(&c)

	// WithConfig replaced the whole block (applied last), so Addr is ":1".
	assert.Equal(t, ":1", c.Addr)
}

func TestWithLogger_NilAllowed(t *testing.T) {
	c := baseConfig()
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	WithLogger(l)(&c)
	assert.Same(t, l, c.logger)

	WithLogger(nil)(&c)
	assert.Nil(t, c.logger)
	assert.Empty(t, c.errs, "nil logger is allowed, not a validation error")
}

func TestCodeOptions_NilAppendError(t *testing.T) {
	t.Run("listener", func(t *testing.T) {
		c := baseConfig()
		WithListener(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
		assert.Nil(t, c.listener)
	})
	t.Run("tlsconfig", func(t *testing.T) {
		c := baseConfig()
		WithTLSConfig(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
	t.Run("basecontext", func(t *testing.T) {
		c := baseConfig()
		WithBaseContext(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
	t.Run("connstate", func(t *testing.T) {
		c := baseConfig()
		WithConnState(nil)(&c)
		require.Len(t, c.errs, 1)
		assert.ErrorIs(t, c.errs[0], ErrInvalidConfig)
	})
}

func TestCodeOptions_StoreNonNil(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	c := baseConfig()
	WithListener(ln)(&c)
	WithTLSConfig(&tls.Config{})(&c)
	WithBaseContext(func() context.Context { return context.Background() })(&c)
	WithConnState(func(net.Conn, http.ConnState) {})(&c)

	assert.Empty(t, c.errs)
	assert.Same(t, ln, c.listener)
	assert.NotNil(t, c.tlsConfig)
	assert.NotNil(t, c.baseContext)
	assert.NotNil(t, c.connState)
}
