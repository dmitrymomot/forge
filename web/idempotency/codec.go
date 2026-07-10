package idempotency

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
)

// Stored-record kinds. A processing marker is a single byte; a done record holds
// the fingerprint, status, filtered headers, and body.
const (
	kindProcessing byte = 0
	kindDone       byte = 1
)

type stored struct {
	header http.Header
	body   []byte
	fp     [32]byte
	status int
	kind   byte
}

func encodeProcessing() []byte { return []byte{kindProcessing} }

// encodeDone serializes a completed response with a length-prefixed layout:
// kind | fp[32] | status(u32) | pairCount(u32) | (keyLen key valLen val)* | bodyLen(u32) | body.
func encodeDone(fp [32]byte, status int, header http.Header, body []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(kindDone)
	b.Write(fp[:])
	writeUint32(&b, uint32(status))
	pairs := 0
	for _, vs := range header {
		pairs += len(vs)
	}
	writeUint32(&b, uint32(pairs))
	for k, vs := range header {
		for _, v := range vs {
			writeString(&b, k)
			writeString(&b, v)
		}
	}
	// Safe to narrow to u32: request/response bodies are bounded by maxBody
	// upstream, well below 4 GiB.
	writeUint32(&b, uint32(len(body)))
	b.Write(body)
	return b.Bytes()
}

func decode(data []byte) (stored, error) {
	if len(data) == 0 {
		return stored{}, ErrCorruptRecord
	}
	switch data[0] {
	case kindProcessing:
		if len(data) != 1 {
			return stored{}, ErrCorruptRecord
		}
		return stored{kind: kindProcessing}, nil
	case kindDone:
		// fall through to parse below
	default:
		return stored{}, ErrCorruptRecord
	}

	r := bytes.NewReader(data[1:])
	s := stored{kind: kindDone, header: http.Header{}}
	if _, err := io.ReadFull(r, s.fp[:]); err != nil {
		return stored{}, ErrCorruptRecord
	}
	status, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	s.status = int(status)
	pairs, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	for range pairs {
		k, err := readString(r)
		if err != nil {
			return stored{}, ErrCorruptRecord
		}
		v, err := readString(r)
		if err != nil {
			return stored{}, ErrCorruptRecord
		}
		s.header.Add(k, v)
	}
	blen, err := readUint32(r)
	if err != nil {
		return stored{}, ErrCorruptRecord
	}
	if int64(blen) > int64(r.Len()) {
		return stored{}, ErrCorruptRecord
	}
	s.body = make([]byte, blen)
	if _, err := io.ReadFull(r, s.body); err != nil {
		return stored{}, ErrCorruptRecord
	}
	if r.Len() != 0 {
		return stored{}, ErrCorruptRecord
	}
	return s, nil
}

func writeUint32(b *bytes.Buffer, n uint32) {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], n)
	b.Write(tmp[:])
}

func writeString(b *bytes.Buffer, s string) {
	writeUint32(b, uint32(len(s)))
	b.WriteString(s)
}

func readUint32(r *bytes.Reader) (uint32, error) {
	var tmp [4]byte
	if _, err := io.ReadFull(r, tmp[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(tmp[:]), nil
}

func readString(r *bytes.Reader) (string, error) {
	n, err := readUint32(r)
	if err != nil {
		return "", err
	}
	if int64(n) > int64(r.Len()) {
		return "", io.ErrUnexpectedEOF
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
