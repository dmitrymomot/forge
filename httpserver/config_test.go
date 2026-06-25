package httpserver

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, 15*time.Second, cfg.ShutdownTimeout)
	assert.Equal(t, 10*time.Second, cfg.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, cfg.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout)
	assert.Equal(t, 120*time.Second, cfg.IdleTimeout)
	assert.Equal(t, 1<<20, cfg.MaxHeaderBytes)
	assert.Empty(t, cfg.Name, "Name defaults empty so Name() derives it")
	assert.Empty(t, cfg.TLSCertFile)
	assert.Empty(t, cfg.TLSKeyFile)
	require.NoError(t, cfg.Validate())
}

func TestConfig_Validate(t *testing.T) {
	tests := map[string]Config{
		"empty addr":      {Addr: ""},
		"neg shutdown":    {Addr: ":0", ShutdownTimeout: -1},
		"neg read header": {Addr: ":0", ReadHeaderTimeout: -1},
		"neg read":        {Addr: ":0", ReadTimeout: -1},
		"neg write":       {Addr: ":0", WriteTimeout: -1},
		"neg idle":        {Addr: ":0", IdleTimeout: -1},
		"neg maxheader":   {Addr: ":0", MaxHeaderBytes: -1},
		"half tls (cert)": {Addr: ":0", TLSCertFile: "c.pem"},
		"half tls (key)":  {Addr: ":0", TLSKeyFile: "k.pem"},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			err := cfg.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidConfig)
		})
	}

	// Both TLS files set is valid; zero timeouts are valid (disabled/indefinite).
	require.NoError(t, Config{Addr: ":0", TLSCertFile: "c.pem", TLSKeyFile: "k.pem"}.Validate())
	require.NoError(t, Config{Addr: ":0", WriteTimeout: 0}.Validate())
}

func TestConfig_EnvTags(t *testing.T) {
	want := map[string]string{
		"Addr":              "ADDR",
		"Name":              "NAME",
		"ShutdownTimeout":   "SHUTDOWN_TIMEOUT",
		"ReadHeaderTimeout": "READ_HEADER_TIMEOUT",
		"ReadTimeout":       "READ_TIMEOUT",
		"WriteTimeout":      "WRITE_TIMEOUT",
		"IdleTimeout":       "IDLE_TIMEOUT",
		"MaxHeaderBytes":    "MAX_HEADER_BYTES",
		"TLSCertFile":       "TLS_CERT_FILE",
		"TLSKeyFile":        "TLS_KEY_FILE",
	}
	typ := reflect.TypeFor[Config]()
	for fname, tag := range want {
		f, ok := typ.FieldByName(fname)
		require.Truef(t, ok, "field %s missing", fname)
		assert.Equalf(t, tag, f.Tag.Get("env"), "field %s env tag", fname)
	}
}
