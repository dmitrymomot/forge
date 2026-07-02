package filetype

import "bytes"

// signature is one entry in the curated magic-byte table. It matches when the
// input starts with prefix (or, when offset > 0, contains prefix at offset),
// and — for RIFF/ftyp containers that share a leading tag — when the optional
// sub tag is present at subAt. A nil match func on a signature is not used;
// container disambiguation is handled by the fields below.
type signature struct {
	typ    Type
	prefix []byte // required leading (or offset) bytes
	sub    []byte // optional secondary tag (e.g. "WEBP", "ftyp" brand)
	offset int    // where prefix must appear (0 = start)
	subAt  int    // where sub must appear
}

func (s signature) match(head []byte) bool {
	if s.offset+len(s.prefix) > len(head) {
		return false
	}
	if !bytes.Equal(head[s.offset:s.offset+len(s.prefix)], s.prefix) {
		return false
	}
	if len(s.sub) == 0 {
		return true
	}
	if s.subAt+len(s.sub) > len(head) {
		return false
	}
	return bytes.Equal(head[s.subAt:s.subAt+len(s.sub)], s.sub)
}

// signatures is the curated table, ordered most-specific first so RIFF/ftyp
// container sub-tags win before any broader prefix. Pure data — no I/O.
var signatures = []signature{
	// images
	{typ: Type{"image/png", "png"}, prefix: []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	{typ: Type{"image/jpeg", "jpg"}, prefix: []byte{0xFF, 0xD8, 0xFF}},
	{typ: Type{"image/gif", "gif"}, prefix: []byte("GIF87a")},
	{typ: Type{"image/gif", "gif"}, prefix: []byte("GIF89a")},
	// WEBP: "RIFF" then "WEBP" at offset 8.
	{typ: Type{"image/webp", "webp"}, prefix: []byte("RIFF"), sub: []byte("WEBP"), subAt: 8},
	{typ: Type{"image/bmp", "bmp"}, prefix: []byte("BM")},
	{typ: Type{"image/tiff", "tiff"}, prefix: []byte{'I', 'I', 0x2A, 0x00}},
	{typ: Type{"image/tiff", "tiff"}, prefix: []byte{'M', 'M', 0x00, 0x2A}},
	{typ: Type{"image/x-icon", "ico"}, prefix: []byte{0x00, 0x00, 0x01, 0x00}},

	// documents / archives
	{typ: Type{"application/pdf", "pdf"}, prefix: []byte("%PDF-")},
	{typ: Type{"application/zip", "zip"}, prefix: []byte{'P', 'K', 0x03, 0x04}},
	{typ: Type{"application/gzip", "gz"}, prefix: []byte{0x1F, 0x8B}},
	// TAR: "ustar" magic at offset 257.
	{typ: Type{"application/x-tar", "tar"}, prefix: []byte("ustar"), offset: 257},

	// audio
	{typ: Type{"audio/mpeg", "mp3"}, prefix: []byte("ID3")},
	{typ: Type{"audio/mpeg", "mp3"}, prefix: []byte{0xFF, 0xFB}},
	{typ: Type{"audio/mpeg", "mp3"}, prefix: []byte{0xFF, 0xF3}},
	{typ: Type{"audio/mpeg", "mp3"}, prefix: []byte{0xFF, 0xF2}},
	// WAV: "RIFF" then "WAVE" at offset 8.
	{typ: Type{"audio/wav", "wav"}, prefix: []byte("RIFF"), sub: []byte("WAVE"), subAt: 8},
	{typ: Type{"audio/ogg", "ogg"}, prefix: []byte("OggS")},
	{typ: Type{"audio/flac", "flac"}, prefix: []byte("fLaC")},

	// video
	// MP4/MOV share the "ftyp" box at offset 4; the brand at offset 8 splits them.
	{typ: Type{"video/quicktime", "mov"}, prefix: []byte("ftyp"), offset: 4, sub: []byte("qt  "), subAt: 8},
	{typ: Type{"video/mp4", "mp4"}, prefix: []byte("ftyp"), offset: 4},
	// WEBM: Matroska/EBML header.
	{typ: Type{"video/webm", "webm"}, prefix: []byte{0x1A, 0x45, 0xDF, 0xA3}},
}
