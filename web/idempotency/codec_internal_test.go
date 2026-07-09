package idempotency

import (
	"errors"
	"net/http"
	"testing"
)

func TestEncodeDecodeDone(t *testing.T) {
	fp := [32]byte{1, 2, 3, 31: 9}
	hdr := http.Header{"Content-Type": {"application/json"}, "X-Multi": {"a", "b"}}
	got, err := decode(encodeDone(fp, http.StatusCreated, hdr, []byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != kindDone {
		t.Fatalf("kind = %d, want done", got.kind)
	}
	if got.fp != fp {
		t.Fatalf("fingerprint mismatch")
	}
	if got.status != http.StatusCreated {
		t.Fatalf("status = %d", got.status)
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Fatalf("content-type lost")
	}
	if len(got.header["X-Multi"]) != 2 {
		t.Fatalf("multi-value header lost: %v", got.header["X-Multi"])
	}
	if string(got.body) != "hello" {
		t.Fatalf("body = %q", got.body)
	}
}

func TestDecodeProcessing(t *testing.T) {
	got, err := decode(encodeProcessing())
	if err != nil {
		t.Fatal(err)
	}
	if got.kind != kindProcessing {
		t.Fatalf("want processing marker")
	}
}

func TestDecodeRejectsOversizedLengths(t *testing.T) {
	// kindDone | fp[32] | status(4) | pairCount=0 | bodyLen=0xFFFFFFFF, no body
	rec := []byte{kindDone}
	rec = append(rec, make([]byte, 32)...)    // fp
	rec = append(rec, 0, 0, 0, 200)           // status 200
	rec = append(rec, 0, 0, 0, 0)             // pairCount 0
	rec = append(rec, 0xFF, 0xFF, 0xFF, 0xFF) // bodyLen ~4GiB (no body follows)
	if _, err := decode(rec); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("oversized bodyLen: got %v, want ErrCorruptRecord", err)
	}

	// kindDone | fp[32] | status(4) | pairCount=1 | keyLen=0xFFFFFFFF, nothing after
	rec2 := []byte{kindDone}
	rec2 = append(rec2, make([]byte, 32)...)
	rec2 = append(rec2, 0, 0, 0, 200)
	rec2 = append(rec2, 0, 0, 0, 1)             // pairCount 1
	rec2 = append(rec2, 0xFF, 0xFF, 0xFF, 0xFF) // keyLen ~4GiB
	if _, err := decode(rec2); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("oversized keyLen: got %v, want ErrCorruptRecord", err)
	}
}

func TestDecodeCorrupt(t *testing.T) {
	if _, err := decode(nil); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("nil: %v", err)
	}
	if _, err := decode([]byte{kindDone, 0x01, 0x02}); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("truncated: %v", err)
	}
	if _, err := decode([]byte{0x7f}); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("unknown kind: %v", err)
	}
}
