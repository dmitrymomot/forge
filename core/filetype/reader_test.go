package filetype_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/filetype"
)

func TestDetectReader_DetectsAndReplaysFullStream(t *testing.T) {
	// A PNG header followed by a payload larger than the 512-byte peek window,
	// so we prove the tail past the peek is not lost.
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	payload := bytes.Repeat([]byte("X"), 2000)
	full := append(append([]byte{}, png...), payload...)

	typ, r, err := filetype.DetectReader(bytes.NewReader(full))
	require.NoError(t, err)
	assert.Equal(t, "image/png", typ.MIME)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, full, got, "full stream must be replayable, byte-for-byte")
}

func TestDetectReader_NonSeekableStream(t *testing.T) {
	// strings.Reader is fine, but wrap in an io.Reader-only type to prove we do
	// not rely on Seek.
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	src := readerOnly{bytes.NewReader(append(png, []byte("tail-bytes")...))}

	typ, r, err := filetype.DetectReader(src)
	require.NoError(t, err)
	assert.Equal(t, "image/png", typ.MIME)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, append(png, []byte("tail-bytes")...), got)
}

func TestDetectReader_ShortStream(t *testing.T) {
	// Fewer than 512 bytes: ReadFull returns ErrUnexpectedEOF, which must be
	// swallowed (short files are normal), and the exact bytes replayed.
	data := []byte("GIF89a\x00\x00short")
	typ, r, err := filetype.DetectReader(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "image/gif", typ.MIME)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestDetectReader_EmptyStream(t *testing.T) {
	// Empty input: io.EOF from ReadFull is swallowed; detection yields ok=false
	// (octet-stream), returned as a non-error Type; replay is empty.
	typ, r, err := filetype.DetectReader(bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Equal(t, "application/octet-stream", typ.MIME)

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDetectReader_NilReader(t *testing.T) {
	_, _, err := filetype.DetectReader(nil)
	assert.ErrorIs(t, err, filetype.ErrNilReader)
}

func TestDetectReader_PropagatesReadError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := filetype.DetectReader(errReader{boom})
	assert.ErrorIs(t, err, boom)
}

func TestDetectReader_ComposesWithDetectAndIs(t *testing.T) {
	// End-to-end: a JPEG stream is detected via DetectReader, and the replayed
	// bytes still satisfy Detect and Is identically to the raw head.
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	full := append(append([]byte{}, jpeg...), bytes.Repeat([]byte("d"), 1000)...)

	typ, r, err := filetype.DetectReader(bytes.NewReader(full))
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", typ.MIME)
	assert.Equal(t, "jpg", typ.Ext)

	replayed, err := io.ReadAll(r)
	require.NoError(t, err)

	got, ok := filetype.Detect(replayed)
	assert.True(t, ok)
	assert.Equal(t, typ, got)
	assert.True(t, filetype.Is(replayed, "image/jpeg"))
}

// readerOnly hides any Seeker/WriterTo methods of the wrapped reader.
type readerOnly struct{ r io.Reader }

func (ro readerOnly) Read(p []byte) (int, error) { return ro.r.Read(p) }

// errReader returns err on the first Read.
type errReader struct{ err error }

func (er errReader) Read(p []byte) (int, error) { return 0, er.err }

// Sanity: strings.Reader stays usable as a plain io.Reader input.
var _ io.Reader = strings.NewReader("")
