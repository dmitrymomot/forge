package smartlink

import (
	"context"
	"errors"
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
// Every response — success, dead link, or error — carries
// "Cache-Control: no-store": a redirect decision can change on the next
// rule evaluation or offer update, so nothing here is safe for a client or
// intermediary to cache.
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

		v := Visit{Params: firstQueryValues(r.URL.Query())}
		if m.cfg.visitFunc != nil {
			v = m.cfg.visitFunc(r, v)
		}
		for k, val := range l.Metadata {
			if v.Params == nil {
				v.Params = make(map[string]string, len(l.Metadata))
			}
			v.Params[k] = val
		}

		d, ok := m.decider(ctx, w, r, code, l)
		if !ok {
			return
		}

		dec := d.Decide(v)
		http.Redirect(w, r, dec.URL, m.cfg.redirectStatus)
		if m.cfg.onHit != nil {
			m.cfg.onHit(ctx, Hit{Link: l, Visit: v, Decision: dec})
		}
	})
}

// decider resolves l to a Decider — a per-hit degenerate compile for a
// Target link, or the configured Resolver for a Ref link — writing an error
// response and reporting ok == false when it cannot produce one.
func (m *Manager) decider(ctx context.Context, w http.ResponseWriter, r *http.Request, code string, l Link) (Decider, bool) {
	switch {
	case l.Target != "":
		compiled, err := m.compileTarget(l)
		if err != nil {
			// Unreachable in practice: Create validates Target with the same
			// compile call. Still handled as an internal error, not a 404 —
			// it reflects a configuration/data problem, not a dead link.
			m.cfg.logger.ErrorContext(ctx, "smartlink: compile target", "code", code, "error", err)
			internalServerError(w)
			return nil, false
		}
		return compiled, true

	case m.cfg.resolver == nil:
		m.cfg.logger.ErrorContext(ctx, "smartlink: ref link with no resolver configured", "code", code)
		internalServerError(w)
		return nil, false

	default:
		resolved, err := m.cfg.resolver(ctx, l)
		if err != nil {
			if errors.Is(err, ErrNoTarget) {
				m.deadLink(w, r)
				return nil, false
			}
			m.cfg.logger.ErrorContext(ctx, "smartlink: resolver error", "code", code, "error", err)
			internalServerError(w)
			return nil, false
		}
		return resolved, true
	}
}

// compileTarget compiles the degenerate single-target Spec for a
// Target-backed Link, using the configured link param policy. No caching in
// v1: a single-template compile is a few µs (see bench_test.go), and an
// unbounded per-code compiled cache is a memory liability at affiliate-code
// cardinality.
func (m *Manager) compileTarget(l Link) (Decider, error) {
	return Compile(Spec{Default: []Target{{URL: l.Target}}, Params: m.cfg.linkParamPolicy})
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

// firstQueryValues flattens q to its first value per key — the shape Visit
// wants for ParamEquals, {param.NAME} macros, and the link's param-merge
// policy. A request with no query params yields a nil map, matching Visit's
// zero value.
func firstQueryValues(q url.Values) map[string]string {
	if len(q) == 0 {
		return nil
	}
	out := make(map[string]string, len(q))
	for k, vals := range q {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}
