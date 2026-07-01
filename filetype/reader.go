package filetype

import (
	"bytes"
	"errors"
	"io"
)

// peekSize is the fixed head window DetectReader inspects — the same 512 bytes
// net/http.DetectContentType and every curated signature need.
const peekSize = 512

// DetectReader peeks up to peekSize bytes from r without consuming the stream,
// detects the type from that head, and returns a reader that replays the full
// original stream (the peeked head followed by the remainder of r). It works on
// non-seekable readers and does not depend on iox.
//
// A short stream (fewer than peekSize bytes) is not an error: io.EOF and
// io.ErrUnexpectedEOF from the peek are swallowed and detection runs on the
// bytes available. Any other read error is returned. A nil r returns
// ErrNilReader.
func DetectReader(r io.Reader) (Type, io.Reader, error) {
	if r == nil {
		return Type{}, nil, ErrNilReader
	}
	head := make([]byte, peekSize)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Type{}, nil, err
	}
	head = head[:n]
	typ, _ := Detect(head)
	return typ, io.MultiReader(bytes.NewReader(head), r), nil
}
