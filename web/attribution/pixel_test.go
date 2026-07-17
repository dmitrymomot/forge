package attribution_test

import (
	"bytes"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dmitrymomot/forge/web/attribution"
)

func TestPixelServesValidGIF(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	rec := httptest.NewRecorder()
	tr.Pixel().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pixel.gif", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/gif" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("Cache-Control = %q", cc)
	}
	if rec.Header().Get("Pragma") != "no-cache" || rec.Header().Get("Expires") != "0" {
		t.Fatal("legacy no-cache headers missing")
	}
	body := rec.Body.Bytes()
	if cl := rec.Header().Get("Content-Length"); cl != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length = %q, body %d bytes", cl, len(body))
	}
	img, err := gif.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("body is not a valid GIF: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Fatalf("pixel bounds = %v, want 1x1", b)
	}
}

func TestPixelCapturesParams(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	rec := httptest.NewRecorder()
	tr.Pixel().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pixel.gif?utm_source=email&utm_campaign=july", nil))

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])
	touch, err := tr.Touch(req)
	if err != nil {
		t.Fatal(err)
	}
	if touch.Get("utm_source") != "email" || touch.Get("utm_campaign") != "july" {
		t.Fatalf("pixel-captured params = %v", touch.Params)
	}
}

func TestPixelHeadOmitsBody(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	rec := httptest.NewRecorder()
	tr.Pixel().ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/pixel.gif", nil))

	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body = %d bytes, want none", rec.Body.Len())
	}
	if rec.Header().Get("Content-Type") != "image/gif" || rec.Header().Get("Content-Length") == "" {
		t.Fatal("HEAD must still carry the GIF headers")
	}
}
