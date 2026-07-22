package smartlink_test

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/web/smartlink"
)

func mustCompile(t *testing.T, spec smartlink.Spec, opts ...smartlink.Option) *smartlink.Compiled {
	t.Helper()
	link, err := smartlink.Compile(spec, opts...)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return link
}

func rule(name, url string, when ...smartlink.Matcher) smartlink.Rule {
	return smartlink.Rule{Name: name, When: when, Targets: []smartlink.Target{{URL: url}}}
}

func mustDecide(t *testing.T, l *smartlink.Compiled, v smartlink.Visit) smartlink.Decision {
	t.Helper()
	d, err := l.Decide(v)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	return d
}

// wantMissingFact asserts Decide fails closed with ErrMissingFact and returns
// no decision.
func wantMissingFact(t *testing.T, l *smartlink.Compiled, v smartlink.Visit) {
	t.Helper()
	d, err := l.Decide(v)
	if !errors.Is(err, smartlink.ErrMissingFact) {
		t.Fatalf("Decide() = (%+v, %v), want ErrMissingFact", d, err)
	}
	if d.URL != "" || d.Rule != "" {
		t.Fatalf("Decide() returned a decision %+v alongside the error", d)
	}
}

// TestDecideMissingFactFailsClosed asserts a visit lacking a fact the spec's
// rules consult is refused instead of silently falling through to the default
// target — the restricted-jurisdictions-on-geoip-miss failure class.
func TestDecideMissingFactFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		when smartlink.Matcher
	}{
		{"geo", smartlink.Geo{Countries: []string{"CU", "IR", "KP"}}},
		{"device", smartlink.Device{Devices: []string{"mobile"}}},
		{"locale", smartlink.Locale{Locales: []string{"en"}}},
		{"percent", smartlink.Percent{Share: 30}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			link := mustCompile(t, smartlink.Spec{
				Rules:   []smartlink.Rule{rule("gate", "https://compliance.example.com", tc.when)},
				Default: defTargets(),
			})
			wantMissingFact(t, link, smartlink.Visit{})
		})
	}
}

// TestDecideMissingFactIrrelevantWhenEarlierRuleMatches asserts the gate is
// lazy: a missing fact only errors when the decision depends on it, not
// because a later rule happens to consult it.
func TestDecideMissingFactIrrelevantWhenEarlierRuleMatches(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{
			rule("bot", "https://sinkhole.example.com", smartlink.ParamEquals{Key: "bot", Values: []string{"1"}}),
			rule("geo", "https://geo.example.com", smartlink.Geo{Countries: []string{"DE"}}),
		},
		Default: defTargets(),
	})
	d := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"bot": "1"}})
	if d.Rule != "bot" {
		t.Fatalf("Rule = %q, want bot", d.Rule)
	}
	// Without the earlier match the missing country is load-bearing again.
	wantMissingFact(t, link, smartlink.Visit{})
}

// TestDecideMissingFactIrrelevantWhenRuleDefinitelyFalse asserts a conjunction
// with a definitively-false matcher skips the rule without erroring on a
// missing sibling fact — the outcome is provably identical either way — and
// that matcher order within the conjunction doesn't change that.
func TestDecideMissingFactIrrelevantWhenRuleDefinitelyFalse(t *testing.T) {
	t.Parallel()
	geo := smartlink.Geo{Countries: []string{"DE"}}
	device := smartlink.Device{Devices: []string{"mobile"}}
	for name, when := range map[string][]smartlink.Matcher{
		"geo-first":    {geo, device},
		"device-first": {device, geo},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			link := mustCompile(t, smartlink.Spec{
				Rules:   []smartlink.Rule{{Name: "de-mobile", When: when, Targets: []smartlink.Target{{URL: "https://hit.com"}}}},
				Default: defTargets(),
			})
			// Device is definitively not mobile: the rule cannot match no matter
			// the country, so the missing country must not error.
			d := mustDecide(t, link, smartlink.Visit{Device: "desktop"})
			if d.Rule != "" {
				t.Fatalf("Rule = %q, want default", d.Rule)
			}
			// Device matches, country missing: the rule's outcome now hinges on
			// the missing fact.
			wantMissingFact(t, link, smartlink.Visit{Device: "mobile"})
		})
	}
}

// TestDecideEmptyStickySplitErrors asserts a weighted split refuses an empty
// StickyKey instead of silently collapsing onto the first target.
func TestDecideEmptyStickySplitErrors(t *testing.T) {
	t.Parallel()
	splitTargets := []smartlink.Target{
		{URL: "https://a.com", Weight: 70},
		{URL: "https://b.com", Weight: 30},
	}

	def := mustCompile(t, smartlink.Spec{Default: splitTargets})
	wantMissingFact(t, def, smartlink.Visit{})
	if d := mustDecide(t, def, smartlink.Visit{StickyKey: "visitor-1"}); d.URL == "" {
		t.Fatalf("keyed visit got no decision")
	}

	ruled := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name:    "de",
			When:    []smartlink.Matcher{smartlink.Geo{Countries: []string{"DE"}}},
			Targets: splitTargets,
		}},
		Default: defTargets(),
	})
	wantMissingFact(t, ruled, smartlink.Visit{Country: "DE"})
	// The split is only consulted when its rule matches: a US visit takes the
	// single default target and needs no sticky key.
	if d := mustDecide(t, ruled, smartlink.Visit{Country: "US"}); d.Rule != "" {
		t.Fatalf("Rule = %q, want default", d.Rule)
	}
}

// TestDecideNoFactsNeededNeverErrors asserts the degenerate short link — one
// default target, no rules — still decides every visit, including the empty
// one.
func TestDecideNoFactsNeededNeverErrors(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{Default: defTargets()})
	if d := mustDecide(t, link, smartlink.Visit{}); d.URL != "https://example.com/" {
		t.Fatalf("URL = %q, want default", d.URL)
	}
}

func TestDecideFirstMatchWins(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{
			rule("first", "https://first.com", smartlink.Geo{Countries: []string{"DE"}}),
			rule("second", "https://second.com", smartlink.Geo{Countries: []string{"DE"}}),
		},
		Default: defTargets(),
	})
	d := mustDecide(t, link, smartlink.Visit{Country: "DE"})
	if d.Rule != "first" || d.URL != "https://first.com" {
		t.Fatalf("got (%q, %q), want first rule", d.Rule, d.URL)
	}
}

func TestDecideDefaultFallback(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("de", "https://de.com", smartlink.Geo{Countries: []string{"DE"}})},
		Default: defTargets(),
	})
	d := mustDecide(t, link, smartlink.Visit{Country: "US"})
	if d.Rule != "" || d.URL != "https://example.com/" {
		t.Fatalf("got (%q, %q), want default", d.Rule, d.URL)
	}
	if d.Target.URL != "https://example.com/" {
		t.Fatalf("Target = %+v, want raw default target", d.Target)
	}
}

func TestDecideUnconditionalRule(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("maintenance", "https://sorry.com")},
		Default: defTargets(),
	})
	if d := mustDecide(t, link, smartlink.Visit{}); d.Rule != "maintenance" {
		t.Fatalf("Rule = %q, want maintenance", d.Rule)
	}
}

func TestDecideConjunction(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{rule("de-mobile", "https://hit.com",
			smartlink.Geo{Countries: []string{"DE"}},
			smartlink.Device{Devices: []string{"mobile"}},
		)},
		Default: defTargets(),
	})
	if d := mustDecide(t, link, smartlink.Visit{Country: "DE", Device: "mobile"}); d.Rule != "de-mobile" {
		t.Fatalf("both matchers true: Rule = %q, want de-mobile", d.Rule)
	}
	if d := mustDecide(t, link, smartlink.Visit{Country: "DE", Device: "desktop"}); d.Rule != "" {
		t.Fatalf("one matcher false: Rule = %q, want default", d.Rule)
	}
}

func TestGeoCaseInsensitive(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("geo", "https://hit.com", smartlink.Geo{Countries: []string{"de"}})},
		Default: defTargets(),
	})
	for _, country := range []string{"DE", "de", "De"} {
		if d := mustDecide(t, link, smartlink.Visit{Country: country}); d.Rule != "geo" {
			t.Fatalf("country %q: Rule = %q, want geo", country, d.Rule)
		}
	}
	// An empty country is a missing fact, not a non-match.
	wantMissingFact(t, link, smartlink.Visit{})
}

func TestLocaleMatching(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rule  string
		visit string
		want  bool
	}{
		{"en", "en", true},
		{"en", "en-US", true},
		{"en", "EN-GB", true},
		{"en-US", "en-US", true},
		{"en-us", "EN-US", true},
		{"en-US", "en", false},
		{"en-US", "en-GB", false},
		{"en", "de", false},
	}
	for _, tc := range cases {
		link := mustCompile(t, smartlink.Spec{
			Rules:   []smartlink.Rule{rule("loc", "https://hit.com", smartlink.Locale{Locales: []string{tc.rule}})},
			Default: defTargets(),
		})
		got := mustDecide(t, link, smartlink.Visit{Locale: tc.visit}).Rule == "loc"
		if got != tc.want {
			t.Errorf("rule %q vs visit %q: matched = %v, want %v", tc.rule, tc.visit, got, tc.want)
		}
		// An empty locale is a missing fact, not a non-match.
		wantMissingFact(t, link, smartlink.Visit{})
	}
}

func TestParamEqualsExact(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{rule("src", "https://hit.com",
			smartlink.ParamEquals{Key: "source", Values: []string{"fb", "ig"}},
		)},
		Default: defTargets(),
	})
	if d := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"source": "ig"}}); d.Rule != "src" {
		t.Fatalf("listed value: Rule = %q, want src", d.Rule)
	}
	if d := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"source": "FB"}}); d.Rule != "" {
		t.Fatalf("param match must be case-sensitive")
	}
	if d := mustDecide(t, link, smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("missing param matched")
	}
}

func TestTimeWindow(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	until := from.Add(24 * time.Hour)
	mock := clock.NewMock(from.Add(time.Hour))
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("window", "https://hit.com", smartlink.TimeWindow{From: from, Until: until})},
		Default: defTargets(),
	}, smartlink.WithClock(mock))

	if d := mustDecide(t, link, smartlink.Visit{}); d.Rule != "window" {
		t.Fatalf("inside window: Rule = %q, want window", d.Rule)
	}
	mock.Set(until) // Until is exclusive
	if d := mustDecide(t, link, smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("at Until: Rule = %q, want default", d.Rule)
	}
	mock.Set(from.Add(-time.Second))
	if d := mustDecide(t, link, smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("before From: Rule = %q, want default", d.Rule)
	}
	// Visit.At overrides the clock (click-log replay).
	if d := mustDecide(t, link, smartlink.Visit{At: from.Add(time.Minute)}); d.Rule != "window" {
		t.Fatalf("Visit.At inside window: Rule = %q, want window", d.Rule)
	}
}

func TestTimeWindowOpenEnded(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{
			rule("until-only", "https://until.com", smartlink.TimeWindow{Until: at}),
			rule("from-only", "https://from.com", smartlink.TimeWindow{From: at}),
		},
		Default: defTargets(),
	})
	if d := mustDecide(t, link, smartlink.Visit{At: at.Add(-time.Hour)}); d.Rule != "until-only" {
		t.Fatalf("before at: Rule = %q, want until-only", d.Rule)
	}
	if d := mustDecide(t, link, smartlink.Visit{At: at.Add(time.Hour)}); d.Rule != "from-only" {
		t.Fatalf("after at: Rule = %q, want from-only", d.Rule)
	}
}

func TestPercentDeterministicAndDistributed(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules:   []smartlink.Rule{rule("pct", "https://hit.com", smartlink.Percent{Share: 30})},
		Default: defTargets(),
	})
	// Deterministic: the same sticky key always lands on the same side.
	first := mustDecide(t, link, smartlink.Visit{StickyKey: "visitor-1"}).Rule
	for range 10 {
		if got := mustDecide(t, link, smartlink.Visit{StickyKey: "visitor-1"}).Rule; got != first {
			t.Fatalf("same sticky key flipped: %q then %q", first, got)
		}
	}
	// An empty sticky key is a missing fact: refuse rather than skip the rule.
	wantMissingFact(t, link, smartlink.Visit{})
	// Distribution over many keys approximates the share.
	matched := 0
	const n = 5000
	for i := range n {
		if mustDecide(t, link, smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)}).Rule == "pct" {
			matched++
		}
	}
	if pct := float64(matched) * 100 / n; pct < 26 || pct > 34 {
		t.Fatalf("share 30 matched %.1f%% of keys", pct)
	}
}

func TestWeightedSplit(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{
			{URL: "https://a.com", Weight: 70},
			{URL: "https://b.com", Weight: 30},
		},
	})
	// Deterministic per key.
	first := mustDecide(t, link, smartlink.Visit{StickyKey: "visitor-1"}).URL
	for range 10 {
		if got := mustDecide(t, link, smartlink.Visit{StickyKey: "visitor-1"}).URL; got != first {
			t.Fatalf("same sticky key flipped targets: %q then %q", first, got)
		}
	}
	// An empty sticky key is a missing fact: refuse rather than collapse the
	// split onto its first target.
	wantMissingFact(t, link, smartlink.Visit{})
	// Distribution approximates the weights.
	a := 0
	const n = 5000
	for i := range n {
		if mustDecide(t, link, smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)}).URL == "https://a.com" {
			a++
		}
	}
	if pct := float64(a) * 100 / n; pct < 66 || pct > 74 {
		t.Fatalf("weight 70 target got %.1f%% of keys", pct)
	}
}

func TestMacroRendering(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.com/{country}/land?d={device}&l={locale}&s={param.sub1}"}},
	})
	d := mustDecide(t, link, smartlink.Visit{
		Country: "DE",
		Device:  "mobile",
		Locale:  "de-DE",
		Params:  map[string]string{"sub1": "camp 1&x"},
	})
	want := "https://a.com/DE/land?d=mobile&l=de-DE&s=camp+1%26x"
	if d.URL != want {
		t.Fatalf("URL = %q, want %q", d.URL, want)
	}
	// Empty visit values render as empty substitutions.
	if got := mustDecide(t, link, smartlink.Visit{}).URL; got != "https://a.com//land?d=&l=&s=" {
		t.Fatalf("sparse visit URL = %q", got)
	}
}

func TestMacroPathEscaping(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.com/{param.slug}/x"}},
	})
	d := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"slug": "a b/c?d"}})
	if want := "https://a.com/a%20b%2Fc%3Fd/x"; d.URL != want {
		t.Fatalf("URL = %q, want %q", d.URL, want)
	}
}

func TestMacroAuthorityEscaping(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://cdn-{param.region}.example.com/lp"}},
	})
	// Legitimate per-region subdomains render untouched.
	if got := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"region": "eu-1"}}).URL; got != "https://cdn-eu-1.example.com/lp" {
		t.Fatalf("URL = %q", got)
	}
	// Hostile values cannot introduce userinfo, a port, or end the authority:
	// the rendered URL still parses with a host under example.com.
	for _, hostile := range []string{"evil.com@real.com", "a:9999", "evil.com/x", "e?y", "e#z"} {
		got := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"region": hostile}}).URL
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("region %q rendered unparseable URL %q: %v", hostile, got, err)
		}
		if u.User != nil || u.Port() != "" || !strings.HasSuffix(u.Hostname(), ".example.com") || u.Path != "/lp" {
			t.Fatalf("region %q altered URL structure: %q", hostile, got)
		}
	}
}

func TestMacroSchemeInjection(t *testing.T) {
	t.Parallel()
	// A ':' in the first segment of a relative template must not reparse as a
	// scheme delimiter.
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "{param.p}/lp"}},
	})
	got := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"p": "https://evil.com"}}).URL
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("rendered unparseable URL %q: %v", got, err)
	}
	if u.IsAbs() || u.Host != "" {
		t.Fatalf("relative template became absolute: %q", got)
	}
}

func TestParamPolicies(t *testing.T) {
	t.Parallel()
	target := []smartlink.Target{{URL: "https://a.com/lp?keep=orig"}}
	visit := smartlink.Visit{Params: map[string]string{"keep": "new", "extra": "1", "": "skipme"}}

	drop := mustCompile(t, smartlink.Spec{Default: target})
	if got := mustDecide(t, drop, visit).URL; got != "https://a.com/lp?keep=orig" {
		t.Fatalf("ParamsDrop URL = %q", got)
	}

	// Original target pairs stay first, verbatim; merged params append sorted.
	fill := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsFill})
	if got := mustDecide(t, fill, visit).URL; got != "https://a.com/lp?keep=orig&extra=1" {
		t.Fatalf("ParamsFill URL = %q", got)
	}

	override := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsOverride})
	if got := mustDecide(t, override, visit).URL; got != "https://a.com/lp?keep=new&extra=1" {
		t.Fatalf("ParamsOverride URL = %q", got)
	}

	// No visit params: merge policies leave the URL untouched.
	if got := mustDecide(t, fill, smartlink.Visit{}).URL; got != "https://a.com/lp?keep=orig" {
		t.Fatalf("ParamsFill without params URL = %q", got)
	}
}

func TestPercentAndSplitDecorrelated(t *testing.T) {
	t.Parallel()
	// A rule gating 50% of traffic into a 50/50 split: if the percent gate
	// and the split shared a hash, one split side would starve.
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name: "gate",
			When: []smartlink.Matcher{smartlink.Percent{Share: 50}},
			Targets: []smartlink.Target{
				{URL: "https://a.com", Weight: 50},
				{URL: "https://b.com", Weight: 50},
			},
		}},
		Default: defTargets(),
	})
	a, b := 0, 0
	for i := range 5000 {
		d := mustDecide(t, link, smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)})
		switch d.URL {
		case "https://a.com":
			a++
		case "https://b.com":
			b++
		}
	}
	if a == 0 || b == 0 {
		t.Fatalf("split starved: a=%d b=%d", a, b)
	}
	if ratio := float64(a) / float64(a+b); ratio < 0.42 || ratio > 0.58 {
		t.Fatalf("gated split ratio %.2f, want ~0.5", ratio)
	}
}

func TestDecideConcurrent(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{rule("de", "https://de.com/{country}",
			smartlink.Geo{Countries: []string{"DE"}}, smartlink.Percent{Share: 50})},
		Default: []smartlink.Target{
			{URL: "https://a.com", Weight: 1},
			{URL: "https://b.com", Weight: 1},
		},
	})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				if _, err := link.Decide(smartlink.Visit{Country: "DE", StickyKey: strconv.Itoa(g*1000 + i)}); err != nil {
					t.Errorf("Decide() error = %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestDecisionReportsRawTarget(t *testing.T) {
	t.Parallel()
	tpl := "https://a.com/{country}"
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: tpl}},
	})
	d := mustDecide(t, link, smartlink.Visit{Country: "DE"})
	if d.Target.URL != tpl {
		t.Fatalf("Target.URL = %q, want raw template", d.Target.URL)
	}
	if !strings.Contains(d.URL, "/DE") {
		t.Fatalf("URL = %q, want rendered", d.URL)
	}
}

// TestParamMergePreservesRawPairs asserts the merge policies keep original
// query pairs verbatim — including pairs url.Values cannot round-trip (an
// unencoded '%', a ';' separator) — instead of silently dropping them.
func TestParamMergePreservesRawPairs(t *testing.T) {
	t.Parallel()
	target := []smartlink.Target{{URL: "https://a.com/lp?promo=50%off&a=1;b=2&keep=orig"}}

	fill := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsFill})
	visit := smartlink.Visit{Params: map[string]string{"sub": "123", "keep": "new"}}
	if got, want := mustDecide(t, fill, visit).URL, "https://a.com/lp?promo=50%off&a=1;b=2&keep=orig&sub=123"; got != want {
		t.Fatalf("ParamsFill URL = %q, want %q", got, want)
	}

	override := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsOverride})
	if got, want := mustDecide(t, override, smartlink.Visit{Params: map[string]string{"keep": "new"}}).URL, "https://a.com/lp?promo=50%off&a=1;b=2&keep=new"; got != want {
		t.Fatalf("ParamsOverride URL = %q, want %q", got, want)
	}
}

// TestParamOverrideCollapsesDuplicates asserts overriding a key that appears
// multiple times rewrites the first occurrence and drops the rest, matching
// url.Values.Set semantics.
func TestParamOverrideCollapsesDuplicates(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.com/lp?k=1&k=2&x=9"}},
		Params:  smartlink.ParamsOverride,
	})
	if got, want := mustDecide(t, link, smartlink.Visit{Params: map[string]string{"k": "new"}}).URL, "https://a.com/lp?k=new&x=9"; got != want {
		t.Fatalf("ParamsOverride URL = %q, want %q", got, want)
	}
}

// TestPercentConjunctionComposes asserts two Percent gates in one rule draw
// independently: 50% AND 50% must pass ~25% of keyed traffic, not collapse
// into a single 50% gate.
func TestPercentConjunctionComposes(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name:    "gate",
			When:    []smartlink.Matcher{smartlink.Percent{Share: 50}, smartlink.Percent{Share: 50}},
			Targets: []smartlink.Target{{URL: "https://rule.example.com/"}},
		}},
		Default: []smartlink.Target{{URL: "https://def.example.com/"}},
	})
	const n = 20000
	hits := 0
	for i := range n {
		if mustDecide(t, link, smartlink.Visit{StickyKey: fmt.Sprintf("k%d", i)}).Rule == "gate" {
			hits++
		}
	}
	share := float64(hits) / n
	if share < 0.23 || share > 0.27 {
		t.Fatalf("50%%*50%% conjunction matched %.1f%%, want ~25%% (collapsed gates match 50%%)", share*100)
	}
}

// TestSaltDecorrelatesBucketing asserts two identically-shaped splits with
// different Spec salts assign sticky keys independently, while the same salt
// stays deterministic.
func TestSaltDecorrelatesBucketing(t *testing.T) {
	t.Parallel()
	spec := func(salt string) smartlink.Spec {
		return smartlink.Spec{Salt: salt, Default: []smartlink.Target{
			{URL: "https://one.example.com/", Weight: 1},
			{URL: "https://two.example.com/", Weight: 1},
		}}
	}
	a := mustCompile(t, spec("link-a"))
	a2 := mustCompile(t, spec("link-a"))
	b := mustCompile(t, spec("link-b"))

	const n = 5000
	differ := 0
	for i := range n {
		key := fmt.Sprintf("k%d", i)
		ua := mustDecide(t, a, smartlink.Visit{StickyKey: key}).URL
		if got := mustDecide(t, a2, smartlink.Visit{StickyKey: key}).URL; got != ua {
			t.Fatalf("same salt diverged for key %q: %q vs %q", key, ua, got)
		}
		if mustDecide(t, b, smartlink.Visit{StickyKey: key}).URL != ua {
			differ++
		}
	}
	if frac := float64(differ) / n; frac < 0.25 {
		t.Fatalf("different salts diverge on %.1f%% of keys, want ~50%% (0%% means salts are ignored)", frac*100)
	}
}
