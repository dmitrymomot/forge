package fingerprint

import (
	"crypto/sha512"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/dmitrymomot/forge/resilience/cache"
)

//go:embed assets/probe.js
var probeJS []byte

const cookieName = "fpjs"

// probePayload is the whitelisted, clamped shape accepted from the browser.
type probePayload struct {
	Timezone            string   `json:"timezone"`
	Platform            string   `json:"platform"`
	Canvas              string   `json:"canvas"`
	WebGL               string   `json:"webgl"`
	Languages           []string `json:"languages"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	WebDriver           bool     `json:"webdriver"`
}

func clampStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func normalizeProbe(p probePayload) probePayload {
	p.Timezone = clampStr(p.Timezone, 64)
	p.Platform = clampStr(p.Platform, 40)
	p.Canvas = clampStr(p.Canvas, 64)
	p.WebGL = clampStr(p.WebGL, 64)
	if p.HardwareConcurrency < 0 || p.HardwareConcurrency > 1024 {
		p.HardwareConcurrency = 0
	}
	if len(p.Languages) > 10 {
		p.Languages = p.Languages[:10]
	}
	for i := range p.Languages {
		p.Languages[i] = clampStr(p.Languages[i], 20)
	}
	return p
}

// ProbeSRI returns the "sha384-..." Subresource Integrity value of the served
// probe.js, for the consumer's <script integrity> / CSP.
func (fp *Fingerprinter) ProbeSRI() string {
	sum := sha512.Sum384(probeJS)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// ScriptHandler serves the embedded probe.js with a long immutable cache and an
// ETag equal to the SRI value.
func (fp *Fingerprinter) ScriptHandler() http.Handler {
	sri := fp.ProbeSRI()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", strconv.Quote(sri))
		_, _ = w.Write(probeJS)
	})
}

// IngestHandler verifies the probe token, whitelists+clamps the payload, and
// persists it (cookie by default, or the cache.Store when WithStore is set) so
// the JSCollector can merge it on subsequent requests.
func (fp *Fingerprinter) IngestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		var in struct {
			Token string       `json:"token"`
			Data  probePayload `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		claims, err := fp.verifyToken(r, in.Token)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		payload, _ := json.Marshal(normalizeProbe(in.Data))
		if fp.store != nil {
			if err := fp.store.Set(r.Context(), storeKey(claims.Nonce), payload, cache.WithTTL(fp.cfg.TokenTTL)); err != nil {
				http.Error(w, "store error", http.StatusInternalServerError)
				return
			}
			_ = fp.cookies.SetSigned(w, cookieName, claims.Nonce)
		} else {
			_ = fp.cookies.SetSigned(w, cookieName, base64.RawURLEncoding.EncodeToString(payload))
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func storeKey(nonce string) string { return "fpjs:" + nonce }

type jsCollector struct{ fp *Fingerprinter }

// JSCollector returns a Collector that merges the ingested probe payload back
// onto later requests. It requires the Fingerprinter that served
// IngestHandler, since reading the payload back needs the same cookie codec
// and (optional) cache.Store.
func (fp *Fingerprinter) JSCollector() Collector { return jsCollector{fp: fp} }

func (c jsCollector) Collect(r *http.Request) ([]Component, error) {
	raw, ok := c.fp.readProbe(r)
	if !ok {
		return nil, nil
	}
	var p probePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil
	}
	comps := []Component{
		{Name: "js-webdriver", Value: strconv.FormatBool(p.WebDriver)},
	}
	if p.Timezone != "" {
		comps = append(comps, Component{Name: "js-timezone", Value: p.Timezone})
	}
	if len(p.Languages) > 0 {
		comps = append(comps, Component{Name: "js-languages", Value: strings.Join(p.Languages, ",")})
	}
	if p.Platform != "" {
		comps = append(comps, Component{Name: "js-platform", Value: p.Platform})
	}
	if p.Canvas != "" {
		comps = append(comps, Component{Name: "js-canvas", Value: p.Canvas})
	}
	if p.WebGL != "" {
		comps = append(comps, Component{Name: "js-webgl", Value: p.WebGL})
	}
	return comps, nil
}

func (fp *Fingerprinter) readProbe(r *http.Request) ([]byte, bool) {
	v, err := fp.cookies.GetSigned(r, cookieName)
	if err != nil || v == "" {
		return nil, false
	}
	if fp.store != nil {
		raw, err := fp.store.Get(r.Context(), storeKey(v))
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return nil, false
	}
	return raw, true
}
