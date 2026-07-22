package smartlink

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strings"
)

// Handler returns an http.Handler implementing the uniform redirect
// pipeline for both Target- and Ref-backed Links: resolve the code (cache
// read-through + liveness), build the Visit, decide, redirect, and fire
// OnHit synchronously after the redirect is written.
//
// The code comes from r.PathValue("code") when the Handler is mounted on a
// pattern with a {code} wildcard (e.g. mux.Handle("/{code}", m.Handler()));
// otherwise it falls back to the trimmed request path, so the Handler also
// works mounted bare at "/".
//
// A code that could never have been minted by [Manager.Create] is answered
// as a dead link without a Store roundtrip — [Manager.Resolve] rejects it as
// [ErrNotFound] before touching the Store — so scans and garbage paths cost
// nothing.
//
// Every response — success, dead link, or error — carries
// "Cache-Control: no-store": a redirect decision can change on the next
// rule evaluation or offer update, so nothing here is safe for a client or
// intermediary to cache.
//
// A visit the Decider refuses is never redirected: [ErrMissingFact] — the
// link's rules consult a fact the visit doesn't carry, e.g. a Geo rule on a
// geoip miss or a weighted split with no sticky key — answers 403 (logged at
// warn), and any other Decider error answers 500. Fail-closed by design:
// neither the default target nor [WithFallbackURL] is a safe destination for
// a click the rules couldn't evaluate. A consumer that wants a friendlier
// outcome (a compliance interstitial, a consent page) wraps the Decider with
// a [Decorator] that catches [ErrMissingFact] and returns its own Decision.
//
// The Handler redirects for every HTTP method, including HEAD, and
// [WithOnHit] fires for all of them alike — it does not special-case
// method. A consumer that needs method awareness (e.g. to skip counting
// HEAD as a click) stamps the method into the Visit itself via
// [WithVisitFunc], e.g. into Visit.Params, and reads it back from the Hit.
func (m *Manager) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.PathValue("code")
		if code == "" {
			code = strings.Trim(r.URL.Path, "/")
		}
		w.Header().Set("Cache-Control", "no-store")

		ctx := r.Context()
		l, err := m.Resolve(ctx, code)
		if err != nil {
			m.resolveFailed(ctx, w, r, code, err)
			return
		}

		v := Visit{Params: firstQueryValues(r.URL.RawQuery)}
		if m.cfg.visitFunc != nil {
			v = m.cfg.visitFunc(r, v)
		}
		if len(l.Metadata) > 0 {
			// Metadata wins. With a VisitFunc configured the merge goes into a
			// fresh map — the map a VisitFunc returned is never mutated behind
			// the consumer's back; without one v.Params is the handler's own
			// map from firstQueryValues, merged in place to keep the hot path
			// allocation-free.
			if m.cfg.visitFunc != nil {
				merged := make(map[string]string, len(v.Params)+len(l.Metadata))
				maps.Copy(merged, v.Params)
				v.Params = merged
			} else if v.Params == nil {
				v.Params = make(map[string]string, len(l.Metadata))
			}
			maps.Copy(v.Params, l.Metadata)
		}

		d, ok := m.decider(ctx, w, r, code, l)
		if !ok {
			return
		}

		dec, err := d.Decide(v)
		if err != nil {
			// Fail closed: a click the engine refuses to route — a Geo rule
			// with no country fact, a split with no sticky key — must never
			// fall back to a redirect (the fallback URL is a destination, not
			// a safe harbor). 403 for the per-visit data gap, 500 for
			// anything else (a decorator failure is a consumer bug/outage).
			if errors.Is(err, ErrMissingFact) {
				m.cfg.logger.WarnContext(ctx, "smartlink: visit refused", "code", code, "error", err)
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				return
			}
			m.cfg.logger.ErrorContext(ctx, "smartlink: decide error", "code", code, "error", err)
			internalServerError(w)
			return
		}
		http.Redirect(w, r, dec.URL, m.cfg.redirectStatus)
		if m.cfg.onHit != nil {
			// Cancellation-detached: a client that disconnects right after
			// reading the redirect must not cancel hit delivery (a queue push
			// with the request ctx would silently lose the click).
			m.cfg.onHit(context.WithoutCancel(ctx), Hit{Link: l, Visit: v, Decision: dec})
		}
	})
}

// decider resolves l to a Decider — a per-hit degenerate compile for a
// Target link, or the configured Resolver for a Ref link, either wrapped in
// the [WithDecorators] chain — writing an error response and reporting
// ok == false when it cannot produce one.
func (m *Manager) decider(ctx context.Context, w http.ResponseWriter, r *http.Request, code string, l Link) (Decider, bool) {
	switch {
	case l.Target != "":
		// Per-hit degenerate compile, no caching in v1: a single-template
		// compile is ~1µs (see bench_test.go), and an unbounded per-code
		// compiled cache is a memory liability at affiliate-code cardinality.
		compiled, err := m.compileLink(l.Code, l.Target)
		if err != nil {
			// Rows created through this Manager cannot fail here — Create
			// validates Target with the same compile call. Rows written to the
			// Store directly (migrations, another Manager with different
			// options, admin tooling) can, so it stays an internal error, not
			// a 404 — a configuration/data problem, not a dead link.
			m.cfg.logger.ErrorContext(ctx, "smartlink: compile target", "code", code, "error", err)
			internalServerError(w)
			return nil, false
		}
		return m.decorated(compiled), true

	case m.cfg.resolver == nil:
		m.cfg.logger.ErrorContext(ctx, "smartlink: ref link with no resolver configured", "code", code)
		internalServerError(w)
		return nil, false

	default:
		resolved, err := m.cfg.resolver(ctx, l)
		if err != nil {
			if refGone(err) {
				m.deadLink(w, r)
				return nil, false
			}
			m.cfg.logger.ErrorContext(ctx, "smartlink: resolver error", "code", code, "error", err)
			internalServerError(w)
			return nil, false
		}
		// A (nil, nil) resolver result would panic in Decide; treat it like a
		// resolver error — a consumer bug is an internal error, not a dead link.
		if resolved == nil {
			m.cfg.logger.ErrorContext(ctx, "smartlink: resolver returned nil Decider without error", "code", code)
			internalServerError(w)
			return nil, false
		}
		return m.decorated(resolved), true
	}
}

// decorated wraps d in the WithDecorators chain, if one is configured.
func (m *Manager) decorated(d Decider) Decider {
	if m.decorate == nil {
		return d
	}
	return m.decorate(d)
}

// resolveFailed maps a Resolve error to the dead-link path (fallback
// redirect or 404) for the expected sentinels, or a logged 500 for anything
// else — an outage must read as an outage, not as every link being gone.
func (m *Manager) resolveFailed(ctx context.Context, w http.ResponseWriter, r *http.Request, code string, err error) {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLinkExpired) || errors.Is(err, ErrLinkDeactivated) {
		m.deadLink(w, r)
		return
	}
	m.cfg.logger.ErrorContext(ctx, "smartlink: resolve error", "code", code, "error", err)
	internalServerError(w)
}

// deadLink redirects to the configured fallback URL, or answers 404 without one.
func (m *Manager) deadLink(w http.ResponseWriter, r *http.Request) {
	if m.cfg.fallbackURL != "" {
		http.Redirect(w, r, m.cfg.fallbackURL, m.cfg.redirectStatus)
		return
	}
	http.NotFound(w, r)
}

// internalServerError writes a plain 500 response.
func internalServerError(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// firstQueryValues extracts the first value per key from the raw query in a
// single pass — the shape Visit wants for ParamEquals, {param.NAME} macros,
// and the link's param-merge policy — without materializing an intermediate
// url.Values (a map plus a slice per key, discarded immediately, on every
// redirect). Pairs url.ParseQuery would reject (';' separators, undecodable
// escapes) are skipped, matching r.URL.Query()'s partial-parse behavior;
// empty-key pairs ("?=foo") are additionally dropped on purpose — no
// consumer (ParamEquals, {param.NAME}, the param merge) can act on an
// empty key. A request with no usable query params yields a nil map,
// matching Visit's zero value.
func firstQueryValues(rawQuery string) map[string]string {
	if rawQuery == "" {
		return nil
	}
	var out map[string]string
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" || strings.ContainsRune(pair, ';') {
			continue
		}
		rawK, rawV, _ := strings.Cut(pair, "=")
		k, err := url.QueryUnescape(rawK)
		if err != nil || k == "" {
			continue
		}
		if _, ok := out[k]; ok {
			continue // first value per key wins, like url.Values ordering
		}
		v, err := url.QueryUnescape(rawV)
		if err != nil {
			continue
		}
		if out == nil {
			out = make(map[string]string, 4)
		}
		out[k] = v
	}
	return out
}
