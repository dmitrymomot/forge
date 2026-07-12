package tlsprint

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

var connKey = ctxkey.New[*Conn]("tlsprint-conn")

// maxClientHelloRecord is the largest TLS record body peek will buffer: the
// protocol's own max record payload, and the documented "ClientHello fits one
// record" bound. A declared length beyond this is never read into memory here
// (see peek); the tls.Server handshake still reads the record itself and
// rejects an oversized one on its own terms.
const maxClientHelloRecord = 16384

// Conn wraps an accepted connection, peeks the TLS ClientHello on the first Read
// (before the tls.Server handshake consumes it), computes its JA4, and then
// replays the buffered bytes transparently.
type Conn struct {
	net.Conn
	prefix *bytes.Reader
	ja4    string
	once   sync.Once
	mu     sync.RWMutex
}

type listener struct{ net.Listener }

// Listener wraps ln so each accepted connection is a *Conn that captures a JA4.
// Pair it with ConnContext on your http.Server.
//
// The wrapper reads the ClientHello during the TLS handshake using the
// connection's existing deadline; it never sets a deadline of its own, so it
// can't strip the server's handshake timeout. Callers MUST set
// http.Server.ReadTimeout (and/or ReadHeaderTimeout) to bound that read and
// prevent a slow-loris client from holding the connection open — the same
// requirement as any TLS server. A declared record length over 16 KiB (see
// maxClientHelloRecord) is not buffered; JA4 capture degrades to empty rather
// than reading an attacker-controlled amount into memory.
func Listener(ln net.Listener) net.Listener { return listener{Listener: ln} }

func (l listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: c}, nil
}

func (c *Conn) Read(p []byte) (int, error) {
	c.once.Do(c.peek)
	if c.prefix != nil && c.prefix.Len() > 0 {
		n, _ := c.prefix.Read(p)
		return n, nil
	}
	return c.Conn.Read(p)
}

// peek reads the first TLS record (the ClientHello), computes JA4, and buffers
// the bytes for replay. Any read/parse failure leaves ja4 empty and replays what
// was read, so a malformed hello never breaks the connection.
func (c *Conn) peek() {
	header := make([]byte, 5)
	if n, err := io.ReadFull(c.Conn, header); err != nil {
		// Replay whatever bytes actually arrived so a short read never drops
		// them from the stream the tls.Server handshake will read next.
		c.prefix = bytes.NewReader(header[:n])
		return
	}
	recLen := int(header[3])<<8 | int(header[4])
	if recLen > maxClientHelloRecord {
		// Declared record body exceeds the one-record ClientHello bound: don't
		// allocate/read it here (that would be an attacker-controlled buffer up
		// to 65535 bytes). Replay just the header; the tls.Server handshake
		// reads the full record byte-identically from the wire and rejects an
		// over-max TLS record itself, so this degrades to an empty JA4 rather
		// than breaking or hanging the connection.
		c.prefix = bytes.NewReader(header)
		return
	}
	body := make([]byte, recLen)
	n, _ := io.ReadFull(c.Conn, body)
	full := append(append([]byte{}, header...), body[:n]...)
	c.prefix = bytes.NewReader(full)
	if h, err := parseClientHello(full); err == nil {
		s := ja4(h)
		c.mu.Lock()
		c.ja4 = s
		c.mu.Unlock()
	}
}

// JA4 returns the captured fingerprint (empty until the handshake has been read).
func (c *Conn) JA4() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ja4
}

// ConnContext stores the *Conn in the request context. Assign it to
// http.Server.ConnContext so Local can retrieve the JA4 at request time.
//
// http.Server.ServeTLS wraps the listener's connection in a *tls.Conn (via
// tls.NewListener) before calling ConnContext, so the argument is usually a
// *tls.Conn rather than the *Conn directly; NetConn unwraps that one layer.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if fc := asConn(c); fc != nil {
		return connKey.With(ctx, fc)
	}
	return ctx
}

// asConn recovers the *Conn from an accepted connection, unwrapping a *tls.Conn
// layer when the server terminated TLS itself.
func asConn(c net.Conn) *Conn {
	switch v := c.(type) {
	case *Conn:
		return v
	case *tls.Conn:
		if fc, ok := v.NetConn().(*Conn); ok {
			return fc
		}
	}
	return nil
}

type localCollector struct{}

// Local returns a Collector that emits the JA4 captured by the Listener for this
// request's connection (via ConnContext). Absent capture contributes nothing.
func Local() fingerprint.Collector { return localCollector{} }

func (localCollector) Collect(r *http.Request) ([]fingerprint.Component, error) {
	c, ok := connKey.From(r.Context())
	if !ok {
		return nil, nil
	}
	if s := c.JA4(); s != "" {
		return []fingerprint.Component{{Name: "tls", Value: s}}, nil
	}
	return nil, nil
}
