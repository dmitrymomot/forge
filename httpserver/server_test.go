package httpserver

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopHandler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
}

func TestNew_SeedsDefaults(t *testing.T) {
	s := New(noopHandler())
	assert.Equal(t, ":8080", s.cfg.Addr)
	assert.Equal(t, 1<<20, s.cfg.MaxHeaderBytes)
	assert.NotNil(t, s.cfg.handler)
}

func TestName_Derivation(t *testing.T) {
	assert.Equal(t, "http :8080", New(noopHandler()).Name())
	assert.Equal(t, "api", New(noopHandler(), WithName("api")).Name())
	assert.Equal(t, "http :9090", New(noopHandler(), WithAddr(":9090")).Name())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	s := New(noopHandler(), WithListener(ln))
	assert.Equal(t, "http "+ln.Addr().String(), s.Name())
}
