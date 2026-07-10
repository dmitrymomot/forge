package idempotency

import (
	"io"
	"net/http/httptest"
	"testing"
)

func TestCaptureBuffersThenFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 1024}
	c.WriteHeader(201)
	_, _ = io.WriteString(c, "body")

	if rec.Body.Len() != 0 {
		t.Fatal("must buffer, not write through before flush")
	}
	if c.finalStatus() != 201 {
		t.Fatalf("finalStatus = %d", c.finalStatus())
	}
	c.flush()
	if rec.Code != 201 || rec.Body.String() != "body" {
		t.Fatalf("after flush: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestCaptureImplicit200(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 1024}
	_, _ = io.WriteString(c, "x") // no explicit WriteHeader
	if c.finalStatus() != 200 {
		t.Fatalf("implicit status = %d, want 200", c.finalStatus())
	}
}

func TestCaptureOverflowStreams(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 4}
	c.WriteHeader(200)
	_, _ = io.WriteString(c, "12345") // exceeds limit 4
	if !c.over {
		t.Fatal("should have flipped to overflow mode")
	}
	if rec.Body.String() != "12345" {
		t.Fatalf("overflow should stream through: %q", rec.Body.String())
	}
	c.flush() // must be a no-op in overflow mode
	if rec.Body.String() != "12345" {
		t.Fatalf("flush double-wrote: %q", rec.Body.String())
	}
}

func TestCaptureMultiWriteOverflowPreservesOrder(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 4}
	c.WriteHeader(200)
	_, _ = io.WriteString(c, "ab") // 2 bytes, buffered (2 <= 4)
	if rec.Body.Len() != 0 {
		t.Fatal("first write must stay buffered, not stream through")
	}
	_, _ = io.WriteString(c, "cde") // 2+3 > 4 => overflow: flush "ab" then stream "cde"
	if !c.over {
		t.Fatal("should have flipped to overflow mode")
	}
	if rec.Body.String() != "abcde" {
		t.Fatalf("overflow must stream buffered bytes then p in order: got %q, want \"abcde\"", rec.Body.String())
	}
	c.flush() // no-op in overflow mode
	if rec.Body.String() != "abcde" {
		t.Fatalf("flush double-wrote: %q", rec.Body.String())
	}
}

func TestCaptureFlushStreamsAndMarksOverflow(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &capture{ResponseWriter: rec, limit: 1024}
	c.WriteHeader(200)
	_, _ = io.WriteString(c, "ab")
	if rec.Body.Len() != 0 {
		t.Fatal("should be buffered before flush")
	}
	c.Flush()
	if !c.over {
		t.Fatal("Flush must flip to overflow mode")
	}
	if rec.Code != 200 || rec.Body.String() != "ab" {
		t.Fatalf("Flush must stream buffered bytes: code=%d body=%q", rec.Code, rec.Body.String())
	}
	_, _ = io.WriteString(c, "cd")
	if rec.Body.String() != "abcd" {
		t.Fatalf("post-flush writes must stream: %q", rec.Body.String())
	}
	c.flush()
	if rec.Body.String() != "abcd" {
		t.Fatalf("internal flush() double-wrote: %q", rec.Body.String())
	}
}
