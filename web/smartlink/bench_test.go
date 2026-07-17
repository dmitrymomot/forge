package smartlink_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/web/smartlink"
)

func benchLink(b *testing.B, spec smartlink.Spec) *smartlink.Compiled {
	b.Helper()
	link, err := smartlink.Compile(spec)
	if err != nil {
		b.Fatalf("Compile() error = %v", err)
	}
	return link
}

// BenchmarkDecideLiteral is the hot path: matcher walk + literal target, no
// rendering, no merge.
func BenchmarkDecideLiteral(b *testing.B) {
	link := benchLink(b, smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name: "de-mobile",
			When: []smartlink.Matcher{
				smartlink.Geo{Countries: []string{"AT", "CH", "DE"}},
				smartlink.Device{Devices: []string{"mobile", "tablet"}},
			},
			Targets: []smartlink.Target{{URL: "https://a.example.com/lp"}},
		}},
		Default: []smartlink.Target{{URL: "https://example.com/"}},
	})
	visit := smartlink.Visit{Country: "de", Device: "mobile", StickyKey: "visitor-42"}
	b.ReportAllocs()
	for b.Loop() {
		link.Decide(visit)
	}
}

// BenchmarkDecideSplit adds a Percent gate and a weighted split — two hash
// buckets per decision.
func BenchmarkDecideSplit(b *testing.B) {
	link := benchLink(b, smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name: "gate",
			When: []smartlink.Matcher{smartlink.Percent{Share: 90}},
			Targets: []smartlink.Target{
				{URL: "https://a.example.com/", Weight: 70},
				{URL: "https://b.example.com/", Weight: 30},
			},
		}},
		Default: []smartlink.Target{{URL: "https://example.com/"}},
	})
	visit := smartlink.Visit{StickyKey: "visitor-42"}
	b.ReportAllocs()
	for b.Loop() {
		link.Decide(visit)
	}
}

// BenchmarkDecideMacro renders a three-macro template.
func BenchmarkDecideMacro(b *testing.B) {
	link := benchLink(b, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.example.com/{country}/lp?d={device}&c={param.click_id}"}},
	})
	visit := smartlink.Visit{
		Country: "DE",
		Device:  "mobile",
		Params:  map[string]string{"click_id": "abc123"},
	}
	b.ReportAllocs()
	for b.Loop() {
		link.Decide(visit)
	}
}

// BenchmarkDecideMerge renders plus re-parses the URL for a ParamsFill merge —
// the most expensive configuration.
func BenchmarkDecideMerge(b *testing.B) {
	link := benchLink(b, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.example.com/lp?keep=orig"}},
		Params:  smartlink.ParamsFill,
	})
	visit := smartlink.Visit{Params: map[string]string{"sub1": "x", "sub2": "y"}}
	b.ReportAllocs()
	for b.Loop() {
		link.Decide(visit)
	}
}

// BenchmarkDecideBare is the floor: a single literal default target, no
// rules, no macros, no merge — everything above this is a matcher, macro, or
// merge cost layered on top.
func BenchmarkDecideBare(b *testing.B) {
	link := benchLink(b, smartlink.Spec{Default: []smartlink.Target{{URL: "https://example.com/"}}})
	visit := smartlink.Visit{StickyKey: "visitor-42"}
	b.ReportAllocs()
	for b.Loop() {
		link.Decide(visit)
	}
}

// noopDecorator wraps d without altering the decision — isolates Chain/call
// overhead from any decorator's own work.
func noopDecorator(d smartlink.Decider) smartlink.Decider {
	return smartlink.DecideFunc(func(v smartlink.Visit) smartlink.Decision {
		return d.Decide(v)
	})
}

// BenchmarkChain3 measures three no-op decorators wrapping a compiled link,
// against BenchmarkDecideBare's bare Decide — the delta is pure Chain/closure
// overhead.
func BenchmarkChain3(b *testing.B) {
	link := benchLink(b, smartlink.Spec{Default: []smartlink.Target{{URL: "https://example.com/"}}})
	chained := smartlink.Chain(noopDecorator, noopDecorator, noopDecorator)(link)
	visit := smartlink.Visit{StickyKey: "visitor-42"}
	b.ReportAllocs()
	for b.Loop() {
		chained.Decide(visit)
	}
}

// BenchmarkMemoryStoreGet is the store hot loop at ~1000 links: MemoryStore.Get
// clones Metadata on every read, so this isolates that cost from the rest of
// the resolve/decide pipeline.
func BenchmarkMemoryStoreGet(b *testing.B) {
	store := smartlink.NewMemoryStore()
	const n = 1000
	codes := make([]string, n)
	ctx := context.Background()
	for i := range codes {
		code := fmt.Sprintf("code-%04d", i)
		codes[i] = code
		l := smartlink.Link{Code: code, Target: "https://example.com/", Metadata: map[string]string{"aff": "abc"}}
		if err := store.Create(ctx, l); err != nil {
			b.Fatalf("Create() error = %v", err)
		}
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if _, err := store.Get(ctx, codes[i%n]); err != nil {
			b.Fatalf("Get() error = %v", err)
		}
		i++
	}
}

// BenchmarkHandlerTarget is the full ServeHTTP loop for a Target-backed
// Link: path/query parse, cache-less lookup against MemoryStore, the per-hit
// degenerate compile ([Manager.decider]), Visit build, Decide, and redirect.
// The recorder and request are recreated inside the loop since
// httptest.ResponseRecorder accumulates header/body state across writes.
func BenchmarkHandlerTarget(b *testing.B) {
	m, err := smartlink.NewManager(smartlink.NewMemoryStore())
	if err != nil {
		b.Fatalf("NewManager() error = %v", err)
	}
	l, err := m.Create(context.Background(), smartlink.CreateParams{
		Target:   "https://dest.example.com/lp?keep=orig",
		Metadata: map[string]string{"aff": "abc123"},
	})
	if err != nil {
		b.Fatalf("Create() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/r/{code}", m.Handler())
	target := "/r/" + l.Code + "?click_id=abc123&sub1=x"

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}
}

// BenchmarkResolveDecideTarget isolates the Target path's product-side cost
// from ServeHTTP/httptest scaffolding (request-line parsing, header
// cloning, response recording, mux routing): Resolve against MemoryStore
// plus the same per-hit degenerate Compile+Decide [Manager.decider] runs,
// with no net/http request/response machinery in the loop.
func BenchmarkResolveDecideTarget(b *testing.B) {
	m, err := smartlink.NewManager(smartlink.NewMemoryStore())
	if err != nil {
		b.Fatalf("NewManager() error = %v", err)
	}
	l, err := m.Create(context.Background(), smartlink.CreateParams{
		Target:   "https://dest.example.com/lp?keep=orig",
		Metadata: map[string]string{"aff": "abc123"},
	})
	if err != nil {
		b.Fatalf("Create() error = %v", err)
	}
	ctx := context.Background()
	visit := smartlink.Visit{Params: map[string]string{"click_id": "abc123", "sub1": "x"}}

	b.ReportAllocs()
	for b.Loop() {
		resolved, err := m.Resolve(ctx, l.Code)
		if err != nil {
			b.Fatalf("Resolve() error = %v", err)
		}
		compiled, err := smartlink.Compile(smartlink.Spec{Default: []smartlink.Target{{URL: resolved.Target}}, Params: smartlink.ParamsFill})
		if err != nil {
			b.Fatalf("Compile() error = %v", err)
		}
		compiled.Decide(visit)
	}
}

// BenchmarkHandlerRef is the same full ServeHTTP loop for a Ref-backed Link
// resolved through a warm [smartlink.Cache] Resolver — the compile cache's
// hit path, not the load-and-compile miss path.
func BenchmarkHandlerRef(b *testing.B) {
	c := smartlink.NewCache(func(_ context.Context, ref string) (smartlink.Spec, error) {
		return smartlink.Spec{Default: []smartlink.Target{{URL: "https://offer.example.com/" + ref}}}, nil
	})
	m, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithResolver(c.Resolver()))
	if err != nil {
		b.Fatalf("NewManager() error = %v", err)
	}
	l, err := m.Create(context.Background(), smartlink.CreateParams{Ref: "offer-42"})
	if err != nil {
		b.Fatalf("Create() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/r/{code}", m.Handler())
	target := "/r/" + l.Code + "?click_id=abc123&sub1=x"

	// Warm the Cache's compiled entry so the timed loop only exercises the
	// RLock hit path, not the load+Compile miss path.
	warmup := httptest.NewRequest(http.MethodGet, target, nil)
	mux.ServeHTTP(httptest.NewRecorder(), warmup)

	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}
}
