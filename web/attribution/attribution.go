package attribution

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/middleware"
)

var touchKey = ctxkey.New[Touch]("attribution_touch")

// maxValueLen caps a single captured param value. Real click IDs stay well
// under it; anything longer is garbage or abuse and is dropped so it cannot
// crowd the 4 KiB cookie limit out from under the legitimate params.
const maxValueLen = 512

// Policy selects which touch wins when a visitor arrives with campaign
// params while a valid touch is already stored.
type Policy int

const (
	// LastTouch overwrites the stored touch on every new campaign visit —
	// the touch closest to conversion wins (the common analytics default).
	LastTouch Policy = iota
	// FirstTouch keeps the original stored touch until it expires — the
	// touch that acquired the visitor wins.
	FirstTouch
)

// Touch is one captured marketing touch: the configured params present on
// the visit and when it happened.
type Touch struct {
	// Params holds the captured query params (e.g. "utm_source" → "google").
	Params map[string]string
	// At is the capture time, second precision, UTC.
	At time.Time
}

// IsZero reports whether t is the zero Touch (nothing captured).
func (t Touch) IsZero() bool { return t.At.IsZero() }

// Get returns the named param, or "" when absent.
func (t Touch) Get(name string) string { return t.Params[name] }

// wireTouch is the compact cookie payload.
type wireTouch struct {
	Params map[string]string `json:"p"`
	At     int64             `json:"at"`
}

// Tracker captures marketing touches into a signed cookie and hands them
// back at conversion time.
type Tracker struct {
	codec *cookie.Codec
	cfg   config
}

// New builds a Tracker over codec. New panics on wiring bugs — a nil codec,
// or a custom __Host- cookie name the codec policy can't satisfy (which
// would otherwise fail every capture silently, since capture is
// best-effort). The default policy is LastTouch with a 30-day window over
// DefaultParams, stored in a "__Host-attribution" cookie ("attribution"
// when the codec policy can't satisfy __Host-).
func New(codec *cookie.Codec, opts ...Option) *Tracker {
	if codec == nil {
		panic("attribution: nil cookie codec")
	}
	cfg := newConfig(opts...)
	if cfg.cookieName == defaultCookieName && !codec.SupportsHostPrefix() {
		cfg.cookieName = "attribution"
	}
	if strings.HasPrefix(cfg.cookieName, "__Host-") && !codec.SupportsHostPrefix() {
		panic("attribution: cookie name " + cfg.cookieName + " requires a codec with Secure, Path=/, and no Domain")
	}
	return &Tracker{codec: codec, cfg: cfg}
}

// Middleware captures configured params from the request query into the
// touch cookie and exposes the effective touch to the handler chain via
// Touch. Requests without campaign params pass through untouched — no
// cookie read or write; a request with no query string at all skips even
// the query parse (zero allocations). Capture is best-effort and never
// fails the request.
func (t *Tracker) Middleware() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if touch := t.capture(w, r); !touch.IsZero() {
				r = r.WithContext(touchKey.With(r.Context(), touch))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Touch returns the stored touch at conversion time: the touch captured on
// this very request (via Middleware or Pixel wiring) or the verified cookie
// from an earlier visit. Absent, tampered, or expired touches return
// ErrNoTouch.
func (t *Tracker) Touch(r *http.Request) (Touch, error) {
	if touch, ok := touchKey.From(r.Context()); ok {
		return touch, nil
	}
	return t.read(r)
}

// Clear expires the touch cookie — call it after the conversion is recorded
// so the same touch is not attributed twice.
func (t *Tracker) Clear(w http.ResponseWriter) { t.codec.Delete(w, t.cfg.cookieName) }

// capture records the request's campaign params per the policy and returns
// the effective touch — the zero Touch when the request carries no
// configured params or nothing valid is stored.
func (t *Tracker) capture(w http.ResponseWriter, r *http.Request) Touch {
	params := t.collect(r)
	if params == nil {
		return Touch{}
	}
	existing, err := t.read(r)
	if err == nil && t.cfg.policy == FirstTouch {
		return existing
	}
	touch := Touch{Params: params, At: t.cfg.clk.Now().UTC().Truncate(time.Second)}
	if werr := t.write(w, touch); werr != nil {
		t.cfg.log.DebugContext(r.Context(), "attribution: touch cookie write failed", slog.Any("error", werr))
		if err == nil {
			return existing
		}
		return Touch{}
	}
	return touch
}

// collect extracts the configured params from the request query. It returns
// nil when none are present.
func (t *Tracker) collect(r *http.Request) map[string]string {
	if r.URL.RawQuery == "" {
		return nil
	}
	query := r.URL.Query()
	var out map[string]string
	for _, name := range t.cfg.params {
		vs, ok := query[name]
		if !ok || len(vs) == 0 || vs[0] == "" {
			continue
		}
		if len(vs[0]) > maxValueLen {
			t.cfg.log.DebugContext(r.Context(), "attribution: param value too long, dropped", slog.String("param", name), slog.Int("len", len(vs[0])))
			continue
		}
		if out == nil {
			out = make(map[string]string, len(t.cfg.params))
		}
		out[name] = vs[0]
	}
	return out
}

func (t *Tracker) read(r *http.Request) (Touch, error) {
	raw, err := t.codec.GetSigned(r, t.cfg.cookieName)
	if err != nil {
		return Touch{}, ErrNoTouch
	}
	var wt wireTouch
	if err := json.Unmarshal([]byte(raw), &wt); err != nil || wt.At <= 0 || len(wt.Params) == 0 {
		return Touch{}, ErrNoTouch
	}
	at := time.Unix(wt.At, 0).UTC()
	if t.cfg.clk.Now().Sub(at) > t.cfg.window {
		return Touch{}, ErrNoTouch
	}
	return Touch{Params: wt.Params, At: at}, nil
}

func (t *Tracker) write(w http.ResponseWriter, touch Touch) error {
	b, err := json.Marshal(wireTouch{At: touch.At.Unix(), Params: touch.Params})
	if err != nil {
		return err
	}
	return t.codec.SetSigned(w, t.cfg.cookieName, string(b), cookie.WithWriteMaxAge(t.cfg.window))
}
