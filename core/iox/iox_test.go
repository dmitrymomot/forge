package iox_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/core/iox"
)

func TestLimitReaderUnderAndAtLimit(t *testing.T) {
	for _, limit := range []int64{5, 10} {
		r := iox.LimitReader(strings.NewReader("hello"), limit)
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("limit %d: unexpected err %v", limit, err)
		}
		if string(b) != "hello" {
			t.Fatalf("limit %d: got %q", limit, b)
		}
	}
}

func TestLimitReaderOverLimit(t *testing.T) {
	r := iox.LimitReader(strings.NewReader("hello world"), 5)
	b, err := io.ReadAll(r)
	if !errors.Is(err, iox.ErrLimitExceeded) {
		t.Fatalf("want ErrLimitExceeded, got %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("want first 5 bytes, got %q", b)
	}
}

func TestLimitReaderZeroAndNegativeLimit(t *testing.T) {
	// n == 0 with data present: any byte exceeds the limit.
	r := iox.LimitReader(strings.NewReader("x"), 0)
	if _, err := io.ReadAll(r); !errors.Is(err, iox.ErrLimitExceeded) {
		t.Fatalf("n=0 with data: want ErrLimitExceeded, got %v", err)
	}
	// n == 0 with empty input: clean EOF, no error.
	r = iox.LimitReader(strings.NewReader(""), 0)
	if b, err := io.ReadAll(r); err != nil || len(b) != 0 {
		t.Fatalf("n=0 empty: got %q, %v", b, err)
	}
	// Negative n must not panic (clamped to 0) and must not return a negative
	// byte count; any input immediately exceeds the limit.
	r = iox.LimitReader(strings.NewReader("hello"), -5)
	if _, err := io.ReadAll(r); !errors.Is(err, iox.ErrLimitExceeded) {
		t.Fatalf("negative n: want ErrLimitExceeded (no panic), got %v", err)
	}
}

func TestDrainClose(t *testing.T) {
	rc := io.NopCloser(strings.NewReader("data"))
	if err := iox.DrainClose(rc); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

type errCloser struct{ err error }

func (e errCloser) Close() error { return e.err }

func TestMultiCloserJoinsErrorsAndSkipsNil(t *testing.T) {
	boom := errors.New("boom")
	c := iox.MultiCloser(errCloser{boom}, errCloser{nil}, nil)
	if err := c.Close(); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestCountingWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := iox.NewCountingWriter(&buf)
	if _, err := cw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := cw.Write([]byte("!")); err != nil {
		t.Fatal(err)
	}
	if cw.N() != 6 {
		t.Fatalf("N = %d, want 6", cw.N())
	}
	if buf.String() != "hello!" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestNopWriteCloser(t *testing.T) {
	var buf bytes.Buffer
	wc := iox.NopWriteCloser(&buf)
	if _, err := wc.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if buf.String() != "x" {
		t.Fatalf("got %q", buf.String())
	}
}
