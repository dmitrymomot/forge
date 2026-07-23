package email_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"math/big"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/email"
)

// testCert issues a self-signed certificate for 127.0.0.1 so STARTTLS and
// implicit-TLS tests verify a real chain instead of skipping verification.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

type smtpServerConfig struct {
	startTLS       bool // advertise and accept STARTTLS
	implicitTLS    bool // TLS handshake immediately on accept
	stall          bool // accept and never speak — for timeout tests
	dropAfterData  bool // 250-accept the DATA body, then sever the connection
	rejectMailCode int  // non-zero: reply this code to MAIL FROM
	rejectRcptCode int  // non-zero: reply this code to every RCPT TO
	rejectAuthCode int  // non-zero: reply this code to AUTH
}

// smtpServer is a minimal in-process SMTP peer capturing what a real server
// would see: EHLO name, AUTH credentials, envelope, and the exact message
// bytes (via DotReader, so dot-stuffing is exercised too).
type smtpServer struct {
	cfg     smtpServerConfig
	ln      net.Listener
	tlsConf *tls.Config

	mu       sync.Mutex
	hello    string
	authLine string
	from     string
	rcpts    []string
	data     []byte
	usedTLS  bool
}

func startSMTPServer(t *testing.T, cfg smtpServerConfig) (*smtpServer, *x509.CertPool) {
	t.Helper()
	cert, pool := testCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &smtpServer{cfg: cfg, ln: ln, tlsConf: &tls.Config{Certificates: []tls.Certificate{cert}}}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s, pool
}

func (s *smtpServer) addr() string { return s.ln.Addr().String() }

func (s *smtpServer) snapshot() (hello, authLine, from string, rcpts []string, data []byte, usedTLS bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hello, s.authLine, s.from, append([]string(nil), s.rcpts...), append([]byte(nil), s.data...), s.usedTLS
}

func (s *smtpServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *smtpServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if s.cfg.stall {
		_, _ = io.Copy(io.Discard, conn) // hold the connection open silently until the client gives up
		return
	}
	secured := false
	if s.cfg.implicitTLS {
		tconn := tls.Server(conn, s.tlsConf)
		if tconn.Handshake() != nil {
			return
		}
		conn = tconn
		secured = true
		s.mu.Lock()
		s.usedTLS = true
		s.mu.Unlock()
	}
	s.session(conn, secured)
}

func (s *smtpServer) session(conn net.Conn, secured bool) {
	tp := textproto.NewConn(conn)
	reply := func(format string, args ...any) bool { return tp.PrintfLine(format, args...) == nil }
	if !reply("220 test.local ESMTP") {
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			s.mu.Lock()
			s.hello = strings.TrimSpace(line[4:])
			s.mu.Unlock()
			reply("250-test.local")
			if s.cfg.startTLS && !secured {
				reply("250-STARTTLS")
			}
			reply("250 AUTH PLAIN")
		case upper == "STARTTLS":
			if !s.cfg.startTLS {
				reply("502 command not implemented")
				continue
			}
			reply("220 ready to start TLS")
			tconn := tls.Server(conn, s.tlsConf)
			if tconn.Handshake() != nil {
				return
			}
			conn = tconn
			secured = true
			tp = textproto.NewConn(tconn)
			s.mu.Lock()
			s.usedTLS = true
			s.mu.Unlock()
		case strings.HasPrefix(upper, "AUTH"):
			s.mu.Lock()
			s.authLine = line
			s.mu.Unlock()
			if s.cfg.rejectAuthCode > 0 {
				reply("%d authentication failed", s.cfg.rejectAuthCode)
				continue
			}
			reply("235 authenticated")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			if s.cfg.rejectMailCode > 0 {
				reply("%d rejected", s.cfg.rejectMailCode)
				continue
			}
			s.mu.Lock()
			s.from = angleAddr(line)
			s.mu.Unlock()
			reply("250 ok")
		case strings.HasPrefix(upper, "RCPT TO:"):
			if s.cfg.rejectRcptCode > 0 {
				reply("%d rejected", s.cfg.rejectRcptCode)
				continue
			}
			s.mu.Lock()
			s.rcpts = append(s.rcpts, angleAddr(line))
			s.mu.Unlock()
			reply("250 ok")
		case upper == "DATA":
			reply("354 end with <CRLF>.<CRLF>")
			body, err := io.ReadAll(tp.DotReader())
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = body
			s.mu.Unlock()
			reply("250 accepted")
			if s.cfg.dropAfterData {
				return // the deferred Close severs the connection before QUIT
			}
		case upper == "QUIT":
			reply("221 bye")
			return
		case upper == "RSET", upper == "NOOP":
			reply("250 ok")
		default:
			reply("500 unrecognized command")
		}
	}
}

func angleAddr(line string) string {
	open := strings.IndexByte(line, '<')
	closing := strings.IndexByte(line, '>')
	if open < 0 || closing <= open {
		return ""
	}
	return line[open+1 : closing]
}

func clientConfig(addr string, pool *x509.CertPool, mode email.TLSMode) (email.Config, email.Option) {
	cfg := email.DefaultConfig()
	cfg.Addr = addr
	cfg.TLS = mode
	cfg.Timeout = 5 * time.Second
	return cfg, email.WithTLSConfig(&tls.Config{RootCAs: pool}) //nolint:gosec // test server's self-signed CA
}

func TestSMTPSendStartTLS(t *testing.T) {
	t.Parallel()
	server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true})
	cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
	cfg.Username = "postmaster"
	cfg.Password = "hunter2"
	cfg.Hello = "app.acme.example"
	sender, err := email.New(cfg, tlsOpt)
	require.NoError(t, err)

	msg := validMessage()
	msg.Cc = []string{"carol@example.com"}
	msg.Bcc = []string{"hidden@example.com"}
	require.NoError(t, sender.Send(context.Background(), msg))

	hello, authLine, from, rcpts, data, usedTLS := server.snapshot()
	assert.Equal(t, "app.acme.example", hello)
	assert.True(t, usedTLS, "session must upgrade via STARTTLS")
	assert.Equal(t, "no-reply@acme.example", from)
	assert.ElementsMatch(t, []string{"ann@example.com", "carol@example.com", "hidden@example.com"}, rcpts)

	require.True(t, strings.HasPrefix(authLine, "AUTH PLAIN "))
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authLine, "AUTH PLAIN "))
	require.NoError(t, err)
	assert.Equal(t, "\x00postmaster\x00hunter2", string(decoded))

	parsed, err := mail.ReadMessage(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "Hello", parsed.Header.Get("Subject"))
	assert.NotContains(t, string(data), "hidden@example.com", "bcc must not appear in transmitted headers")
}

func TestSMTPSendImplicitTLS(t *testing.T) {
	t.Parallel()
	server, pool := startSMTPServer(t, smtpServerConfig{implicitTLS: true})
	cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSImplicit)
	sender, err := email.New(cfg, tlsOpt)
	require.NoError(t, err)

	require.NoError(t, sender.Send(context.Background(), validMessage()))
	_, _, from, _, data, usedTLS := server.snapshot()
	assert.True(t, usedTLS)
	assert.Equal(t, "no-reply@acme.example", from)
	assert.NotEmpty(t, data)
}

func TestSMTPSendPlaintext(t *testing.T) {
	t.Parallel()
	server, _ := startSMTPServer(t, smtpServerConfig{})
	cfg := email.DefaultConfig()
	cfg.Addr = server.addr()
	cfg.TLS = email.TLSNone
	cfg.Username = "dev"
	cfg.Password = "dev"
	sender, err := email.New(cfg)
	require.NoError(t, err)

	require.NoError(t, sender.Send(context.Background(), validMessage()))
	_, authLine, _, _, data, usedTLS := server.snapshot()
	assert.False(t, usedTLS)
	assert.NotEmpty(t, authLine, "PLAIN auth to loopback works without TLS")
	assert.NotEmpty(t, data)
}

func TestSMTPStartTLSUnavailable(t *testing.T) {
	t.Parallel()
	server, pool := startSMTPServer(t, smtpServerConfig{startTLS: false})
	cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
	cfg.Username = "postmaster"
	cfg.Password = "secret"
	sender, err := email.New(cfg, tlsOpt)
	require.NoError(t, err)

	err = sender.Send(context.Background(), validMessage())
	require.ErrorIs(t, err, email.ErrTLSUnavailable)
	_, authLine, from, _, data, _ := server.snapshot()
	assert.Empty(t, authLine, "credentials must never cross plaintext in STARTTLS mode")
	assert.Empty(t, from, "no mail flow after failed upgrade")
	assert.Empty(t, data)
}

func TestSMTPStatusClassification(t *testing.T) {
	t.Parallel()

	t.Run("4xx rcpt is transient", func(t *testing.T) {
		t.Parallel()
		server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true, rejectRcptCode: 450})
		cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
		sender, err := email.New(cfg, tlsOpt)
		require.NoError(t, err)
		err = sender.Send(context.Background(), validMessage())
		require.ErrorIs(t, err, email.ErrTransient)
		assert.NotErrorIs(t, err, email.ErrPermanent)
	})
	t.Run("5xx mail is permanent", func(t *testing.T) {
		t.Parallel()
		server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true, rejectMailCode: 550})
		cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
		sender, err := email.New(cfg, tlsOpt)
		require.NoError(t, err)
		err = sender.Send(context.Background(), validMessage())
		require.ErrorIs(t, err, email.ErrPermanent)
	})
	t.Run("5xx auth is permanent", func(t *testing.T) {
		t.Parallel()
		server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true, rejectAuthCode: 535})
		cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
		cfg.Username = "postmaster"
		cfg.Password = "wrong"
		sender, err := email.New(cfg, tlsOpt)
		require.NoError(t, err)
		err = sender.Send(context.Background(), validMessage())
		require.ErrorIs(t, err, email.ErrPermanent)
	})
}

func TestSMTPAcceptedThenConnectionDrop(t *testing.T) {
	t.Parallel()
	// The server 250-accepts the message body and then severs the connection,
	// so QUIT fails. Send must still return nil: the mail is in flight, and
	// an error here would make an at-least-once queue send it twice.
	server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true, dropAfterData: true})
	cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
	sender, err := email.New(cfg, tlsOpt)
	require.NoError(t, err)

	require.NoError(t, sender.Send(context.Background(), validMessage()))
	_, _, _, _, data, _ := server.snapshot()
	assert.NotEmpty(t, data, "message was accepted before the drop")
}

func TestNewPlaintextAuthNonLocalhost(t *testing.T) {
	t.Parallel()
	cfg := email.DefaultConfig()
	cfg.Addr = "smtp.example.com:587"
	cfg.TLS = email.TLSNone
	cfg.Username = "postmaster"
	cfg.Password = "secret"
	_, err := email.New(cfg)
	require.ErrorIs(t, err, email.ErrInvalidConfig, "PLAIN over plaintext to a remote host can never send")

	cfg.Addr = "localhost:2525"
	_, err = email.New(cfg)
	require.NoError(t, err, "loopback dev catchers stay allowed")

	cfg.Addr = "smtp.example.com:587"
	_, err = email.New(cfg, email.WithAuth(smtp.CRAMMD5Auth("postmaster", "secret")))
	require.NoError(t, err, "WithAuth overrides the PLAIN-over-plaintext guard")
}

func TestSMTPDefaultFrom(t *testing.T) {
	t.Parallel()
	server, pool := startSMTPServer(t, smtpServerConfig{startTLS: true})
	cfg, tlsOpt := clientConfig(server.addr(), pool, email.TLSStartTLS)
	cfg.From = "Acme <no-reply@acme.example>"
	sender, err := email.New(cfg, tlsOpt)
	require.NoError(t, err)

	msg := validMessage()
	msg.From = ""
	require.NoError(t, sender.Send(context.Background(), msg))
	_, _, from, _, _, _ := server.snapshot()
	assert.Equal(t, "no-reply@acme.example", from)
}

func TestSMTPInvalidMessageBeforeDial(t *testing.T) {
	t.Parallel()
	cfg := email.DefaultConfig()
	cfg.Addr = "203.0.113.1:2525" // TEST-NET, nothing listens; validation must fail before any dial
	sender, err := email.New(cfg)
	require.NoError(t, err)

	msg := validMessage()
	msg.To = nil
	start := time.Now()
	err = sender.Send(context.Background(), msg)
	require.ErrorIs(t, err, email.ErrInvalidMessage)
	assert.Less(t, time.Since(start), time.Second)
}

func TestSMTPTimeout(t *testing.T) {
	t.Parallel()
	server, _ := startSMTPServer(t, smtpServerConfig{stall: true})
	cfg := email.DefaultConfig()
	cfg.Addr = server.addr()
	cfg.TLS = email.TLSNone
	cfg.Timeout = 300 * time.Millisecond
	sender, err := email.New(cfg)
	require.NoError(t, err)

	start := time.Now()
	err = sender.Send(context.Background(), validMessage())
	require.Error(t, err)
	assert.NotErrorIs(t, err, email.ErrTransient)
	assert.NotErrorIs(t, err, email.ErrPermanent)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestSMTPZeroValue(t *testing.T) {
	t.Parallel()
	var sender email.SMTP
	err := sender.Send(context.Background(), validMessage())
	require.ErrorContains(t, err, "not constructed")
}

func TestNewConfigValidation(t *testing.T) {
	t.Parallel()
	base := func() email.Config {
		cfg := email.DefaultConfig()
		cfg.Addr = "smtp.example.com:587"
		return cfg
	}
	mutations := map[string]func(*email.Config){
		"missing addr":         func(c *email.Config) { c.Addr = "" },
		"addr without port":    func(c *email.Config) { c.Addr = "smtp.example.com" },
		"unknown tls mode":     func(c *email.Config) { c.TLS = "sometimes" },
		"username without pw":  func(c *email.Config) { c.Username = "u"; c.Password = "" },
		"password without usr": func(c *email.Config) { c.Password = "p" },
		"bad from":             func(c *email.Config) { c.From = "not an address" },
		"non-positive timeout": func(c *email.Config) { c.Timeout = 0 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := base()
			mutate(&cfg)
			_, err := email.New(cfg)
			require.ErrorIs(t, err, email.ErrInvalidConfig)
		})
	}

	sender, err := email.New(base())
	require.NoError(t, err)
	require.NotNil(t, sender)
}
