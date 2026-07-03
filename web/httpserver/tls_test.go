package httpserver_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/httpserver"
)

// selfSigned returns an in-memory cert plus its PEM bytes for the loopback host.
func selfSigned(t *testing.T) (tls.Certificate, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return pair, certPEM, keyPEM
}

// tlsClient trusts exactly the supplied CA/cert PEM — no InsecureSkipVerify.
func tlsClient(t *testing.T, caPEM []byte) *http.Client {
	t.Helper()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(caPEM))
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
}

func startTLS(t *testing.T, opts ...httpserver.Option) (string, <-chan error, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	s := httpserver.New(h, append(opts, httpserver.WithListener(ln))...)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return "https://" + ln.Addr().String(), done, cancel
}

func waitTLS200(t *testing.T, url string, caPEM []byte) {
	t.Helper()
	c := tlsClient(t, caPEM)
	for range 50 {
		resp, err := c.Get(url)
		if err == nil && resp != nil {
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			_ = resp.Body.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("TLS server never served 200")
}

func TestRun_TLSWithConfig(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	url, done, cancel := startTLS(t, httpserver.WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSWithCertFiles(t *testing.T) {
	_, certPEM, keyPEM := selfSigned(t)
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))

	cfg := httpserver.DefaultConfig()
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	url, done, cancel := startTLS(t, httpserver.WithConfig(cfg))
	waitTLS200(t, url, certPEM)
	cancel()
	require.NoError(t, <-done)
}

func TestRun_TLSConfigTakesPrecedenceOverFiles(t *testing.T) {
	cert, certPEM, _ := selfSigned(t)
	// Bogus cert files that would fail to load IF they were used. WithTLSConfig must win.
	cfg := httpserver.DefaultConfig()
	cfg.TLSCertFile = "/nonexistent/cert.pem"
	cfg.TLSKeyFile = "/nonexistent/key.pem"
	url, done, cancel := startTLS(t,
		httpserver.WithConfig(cfg),
		httpserver.WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}),
	)
	waitTLS200(t, url, certPEM) // would fail if the bogus files were loaded
	cancel()
	require.NoError(t, <-done)
}
