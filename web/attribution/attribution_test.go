package attribution_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/web/attribution"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
)

func newCodec(t *testing.T, opts ...cookie.Option) *cookie.Codec {
	t.Helper()
	ks, err := keyset.New(keyset.WithPrimary(1, make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	c, err := cookie.New(ks, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// visit sends a request with cookies through the tracker middleware and
// returns the response recorder plus the touch the handler observed.
func visit(t *testing.T, tr *attribution.Tracker, target string, cookies []*http.Cookie) (*httptest.ResponseRecorder, attribution.Touch, error) {
	t.Helper()
	var (
		seen    attribution.Touch
		seenErr error
	)
	h := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, seenErr = tr.Touch(r)
	}), tr.Middleware())
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, seen, seenErr
}

func TestCaptureAndConvert(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))

	rec, seen, err := visit(t, tr, "/?utm_source=google&utm_medium=cpc&gclid=abc123", nil)
	if err != nil {
		t.Fatalf("Touch on capture request: %v", err)
	}
	if seen.Get("utm_source") != "google" || seen.Get("utm_medium") != "cpc" || seen.Get("gclid") != "abc123" {
		t.Fatalf("captured params = %v", seen.Params)
	}
	if seen.IsZero() || seen.At.IsZero() {
		t.Fatal("captured touch has no timestamp")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-attribution" {
		t.Fatalf("cookies = %v", cookies)
	}

	// Conversion on a later request: cookie round-trip, no middleware needed.
	req := httptest.NewRequest(http.MethodPost, "/signup", nil)
	req.AddCookie(cookies[0])
	got, err := tr.Touch(req)
	if err != nil {
		t.Fatalf("Touch at conversion: %v", err)
	}
	if got.Get("utm_source") != "google" || !got.At.Equal(seen.At) {
		t.Fatalf("stored touch = %+v, want %+v", got, seen)
	}
}

func TestNoParamsNoCookie(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	for _, target := range []string{"/", "/?page=2&sort=asc"} {
		rec, _, err := visit(t, tr, target, nil)
		if !errors.Is(err, attribution.ErrNoTouch) {
			t.Fatalf("%s: Touch err = %v, want ErrNoTouch", target, err)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("%s: unexpected cookie written", target)
		}
	}
}

func TestLastTouchOverwrites(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	tr := attribution.New(newCodec(t), attribution.WithClock(clk))

	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	first := rec.Result().Cookies()

	clk.Advance(time.Hour)
	rec, seen, err := visit(t, tr, "/?utm_source=newsletter&utm_campaign=july", first)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_source") != "newsletter" || seen.Get("utm_campaign") != "july" {
		t.Fatalf("last-touch params = %v", seen.Params)
	}
	if len(rec.Result().Cookies()) != 1 {
		t.Fatal("last-touch visit must rewrite the cookie")
	}

	// The old touch's params are fully replaced, not merged.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	got, err := tr.Touch(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("utm_source") != "newsletter" || len(got.Params) != 2 {
		t.Fatalf("stored touch after overwrite = %v", got.Params)
	}
}

func TestFirstTouchKeepsOriginal(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	tr := attribution.New(newCodec(t), attribution.WithPolicy(attribution.FirstTouch), attribution.WithClock(clk))

	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	first := rec.Result().Cookies()

	clk.Advance(time.Hour)
	rec, seen, err := visit(t, tr, "/?utm_source=newsletter", first)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_source") != "google" {
		t.Fatalf("first-touch source = %q, want original", seen.Get("utm_source"))
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("first-touch keep must not rewrite the cookie")
	}
}

func TestFirstTouchExpiredRecaptures(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	tr := attribution.New(newCodec(t), attribution.WithPolicy(attribution.FirstTouch), attribution.WithWindow(24*time.Hour), attribution.WithClock(clk))

	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	first := rec.Result().Cookies()

	clk.Advance(25 * time.Hour)
	_, seen, err := visit(t, tr, "/?utm_source=newsletter", first)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_source") != "newsletter" {
		t.Fatalf("expired first touch must be replaced, got %q", seen.Get("utm_source"))
	}
}

func TestWindowExpiry(t *testing.T) {
	t.Parallel()
	clk := clock.NewMock(time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	tr := attribution.New(newCodec(t), attribution.WithWindow(24*time.Hour), attribution.WithClock(clk))

	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	ck := rec.Result().Cookies()[0]
	if ck.MaxAge != int((24 * time.Hour).Seconds()) {
		t.Fatalf("cookie MaxAge = %d, want window", ck.MaxAge)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(ck)

	clk.Advance(23 * time.Hour)
	if _, err := tr.Touch(req); err != nil {
		t.Fatalf("inside window: %v", err)
	}
	clk.Advance(2 * time.Hour)
	if _, err := tr.Touch(req); !errors.Is(err, attribution.ErrNoTouch) {
		t.Fatalf("outside window err = %v, want ErrNoTouch", err)
	}
}

func TestTamperedCookieRejected(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	ck := rec.Result().Cookies()[0]

	tampered := *ck
	tampered.Value = strings.Replace(ck.Value, ck.Value[:4], "AAAA", 1)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&tampered)
	if _, err := tr.Touch(req); !errors.Is(err, attribution.ErrNoTouch) {
		t.Fatalf("tampered cookie err = %v, want ErrNoTouch", err)
	}
}

func TestClear(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))
	rec := httptest.NewRecorder()
	tr.Clear(rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "__Host-attribution" || cookies[0].MaxAge >= 0 {
		t.Fatalf("Clear cookies = %v", cookies)
	}
}

func TestParamHygiene(t *testing.T) {
	t.Parallel()
	tr := attribution.New(newCodec(t))

	long := strings.Repeat("x", 600)
	_, seen, err := visit(t, tr, "/?utm_source=google&utm_content="+long+"&utm_medium=&utm_term=a&utm_term=b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_content") != "" {
		t.Fatal("oversized value must be dropped")
	}
	if seen.Get("utm_medium") != "" {
		t.Fatal("empty value must be skipped")
	}
	if seen.Get("utm_term") != "a" {
		t.Fatalf("repeated param = %q, want first value", seen.Get("utm_term"))
	}
	if seen.Get("utm_source") != "google" {
		t.Fatal("valid params must survive hygiene drops")
	}
}

func TestConfiguredParams(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)

	replaced := attribution.New(codec, attribution.WithParams("aff_id", "sub1"))
	_, seen, err := visit(t, replaced, "/?utm_source=google&aff_id=42&sub1=x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_source") != "" || seen.Get("aff_id") != "42" || seen.Get("sub1") != "x" {
		t.Fatalf("WithParams capture = %v", seen.Params)
	}

	extended := attribution.New(codec, attribution.WithExtraParams("ref"))
	_, seen, err = visit(t, extended, "/?utm_source=google&ref=friend", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("utm_source") != "google" || seen.Get("ref") != "friend" {
		t.Fatalf("WithExtraParams capture = %v", seen.Params)
	}
}

func TestCookieNameFallback(t *testing.T) {
	t.Parallel()
	// A codec with a Domain can't satisfy __Host-; the default name falls back.
	tr := attribution.New(newCodec(t, cookie.WithDomain("example.com")))
	rec, _, _ := visit(t, tr, "/?utm_source=google", nil)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "attribution" {
		t.Fatalf("cookies = %v, want fallback name", cookies)
	}

	named := attribution.New(newCodec(t), attribution.WithCookieName("touch"))
	rec, _, _ = visit(t, named, "/?utm_source=google", nil)
	if cookies := rec.Result().Cookies(); len(cookies) != 1 || cookies[0].Name != "touch" {
		t.Fatalf("cookies = %v, want custom name", cookies)
	}
}

func TestOversizedTouchIsBestEffort(t *testing.T) {
	t.Parallel()
	// 17 default params near the per-value cap blow past the 4 KiB cookie
	// limit; capture must drop the touch and never fail the request.
	tr := attribution.New(newCodec(t))
	var sb strings.Builder
	for i, name := range attribution.DefaultParams() {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(name)
		sb.WriteByte('=')
		sb.WriteString(strings.Repeat("v", 500))
	}
	rec, _, err := visit(t, tr, "/?"+sb.String(), nil)
	// The handler observed ErrNoTouch — proof the request reached it despite
	// the dropped write.
	if !errors.Is(err, attribution.ErrNoTouch) {
		t.Fatalf("Touch err = %v, want ErrNoTouch after dropped write", err)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("failed write must not set a cookie")
	}
}

func TestNewPanicsOnNilCodec(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) must panic")
		}
	}()
	attribution.New(nil)
}

func TestNewPanicsOnIncompatibleHostPrefixName(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("custom __Host- name on a Domain-scoped codec must panic")
		}
	}()
	attribution.New(newCodec(t, cookie.WithDomain("example.com")), attribution.WithCookieName("__Host-touch"))
}

func TestParamOptionsDoNotAliasCallerSlice(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	base := make([]string, 1, 2) // spare capacity invites in-place append
	base[0] = "aff_id"

	tr := attribution.New(codec, attribution.WithParams(base...), attribution.WithExtraParams("sub1"))
	if base[:cap(base)][1] == "sub1" {
		t.Fatal("WithExtraParams wrote into the caller's backing array")
	}
	_, seen, err := visit(t, tr, "/?aff_id=42&sub1=x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if seen.Get("aff_id") != "42" || seen.Get("sub1") != "x" {
		t.Fatalf("capture = %v", seen.Params)
	}
}

func TestTouchZeroValue(t *testing.T) {
	t.Parallel()
	var zero attribution.Touch
	if !zero.IsZero() {
		t.Fatal("zero Touch must report IsZero")
	}
	if zero.Get("utm_source") != "" {
		t.Fatal("Get on zero Touch must return empty")
	}
}
