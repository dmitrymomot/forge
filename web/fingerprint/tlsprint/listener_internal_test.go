package tlsprint

import (
	"bytes"
	"net"
	"testing"
)

// TestPeekCapsOversizedRecord drives a *Conn directly over a net.Pipe with a TLS
// record header declaring a body far past maxClientHelloRecord. peek must not
// allocate/read that declared body; it should bail out after the 5-byte header,
// leaving JA4 empty and replaying only the header, with the rest of the wire
// (the bytes actually sent) still readable afterward.
func TestPeekCapsOversizedRecord(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	// Record type 0x16 (handshake), version 0x0301, declared length 60000
	// (0xEA60) -- well past the 16 KiB one-record ClientHello bound.
	header := []byte{0x16, 0x03, 0x01, 0xEA, 0x60}
	body := []byte{0x01, 0x02, 0x03, 0x04} // far short of the declared 60000

	go func() {
		_, _ = client.Write(header)
		_, _ = client.Write(body)
	}()

	c := &Conn{Conn: server}

	buf := make([]byte, 16)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if !bytes.Equal(buf[:n], header) {
		t.Fatalf("expected peek to replay just the %d-byte header, got %d bytes: %x", len(header), n, buf[:n])
	}
	if got := c.JA4(); got != "" {
		t.Fatalf("expected empty JA4 for an oversized declared record length, got %q", got)
	}

	// The connection must still be usable afterward: the tls.Server handshake
	// (or here, our assertion) reads the real bytes on the wire untouched.
	n2, err := c.Read(buf)
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if !bytes.Equal(buf[:n2], body) {
		t.Fatalf("expected the underlying body bytes to still be readable, got %x", buf[:n2])
	}
}
