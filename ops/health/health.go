package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Check reports the health of one dependency; nil means healthy.
type Check func(ctx context.Context) error

// Report is the aggregate result of one scrape.
type Report struct {
	Status string        `json:"status"` // "ok" | "degraded" | "unavailable"
	Checks []CheckResult `json:"checks"`
}

// CheckResult is one check's outcome.
type CheckResult struct {
	Name     string `json:"name"`
	Err      string `json:"err,omitempty"`
	OK       bool   `json:"ok"`
	Critical bool   `json:"critical"`
}

type checkEntry struct {
	check    Check
	name     string
	critical bool
}

type config struct {
	responder func(http.ResponseWriter, *http.Request, Report)
	checks    []checkEntry
	timeout   time.Duration
}

// Option configures a Handler.
type Option func(*config)

type checkConfig struct{ critical bool }

// CheckOption configures a single check.
type CheckOption func(*checkConfig)

// NonCritical marks a check as degrade-not-evict: its failure yields a
// "degraded" 200 instead of a 503.
func NonCritical() CheckOption { return func(c *checkConfig) { c.critical = false } }

// WithCheck registers a named check. Checks are critical by default.
func WithCheck(name string, check Check, opts ...CheckOption) Option {
	cc := checkConfig{critical: true}
	for _, o := range opts {
		o(&cc)
	}
	return func(c *config) {
		c.checks = append(c.checks, checkEntry{name: name, check: check, critical: cc.critical})
	}
}

// WithTimeout bounds each check's context per scrape. 0 inherits the request ctx.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithResponder overrides the default JSON body/format.
func WithResponder(fn func(http.ResponseWriter, *http.Request, Report)) Option {
	return func(c *config) {
		if fn != nil {
			c.responder = fn
		}
	}
}

// Handler returns an http.Handler that runs every registered check on each
// request and reports the aggregate. With no checks it always returns 200 — the
// canonical liveness probe.
func Handler(opts ...Option) http.Handler {
	c := config{responder: defaultResponder}
	for _, o := range opts {
		o(&c)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.responder(w, r, evaluate(r.Context(), c))
	})
}

func evaluate(ctx context.Context, c config) Report {
	results := make([]CheckResult, len(c.checks))
	var wg sync.WaitGroup
	for i, e := range c.checks {
		wg.Go(func() {
			cctx := ctx
			if c.timeout > 0 {
				var cancel context.CancelFunc
				cctx, cancel = context.WithTimeout(ctx, c.timeout)
				defer cancel()
			}
			err := runCheck(cctx, e.check)
			results[i] = CheckResult{Name: e.name, OK: err == nil, Critical: e.critical}
			if err != nil {
				results[i].Err = err.Error()
			}
		})
	}
	wg.Wait()
	return summarize(results)
}

// runCheck invokes check, converting a panic into an error so one bad check
// degrades the report instead of crashing the process.
func runCheck(ctx context.Context, check Check) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return check(ctx)
}

func summarize(results []CheckResult) Report {
	status := "ok"
	for _, r := range results {
		if r.OK {
			continue
		}
		if r.Critical {
			return Report{Status: "unavailable", Checks: results}
		}
		status = "degraded"
	}
	return Report{Status: status, Checks: results}
}

func defaultResponder(w http.ResponseWriter, _ *http.Request, rep Report) {
	code := http.StatusOK
	if rep.Status == "unavailable" {
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(rep)
}
