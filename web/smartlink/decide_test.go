package smartlink_test

import (
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

func TestDecideFirstMatchWins(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Rules: []smartlink.Rule{
			rule("first", "https://first.com", smartlink.Geo{Countries: []string{"DE"}}),
			rule("second", "https://second.com", smartlink.Geo{Countries: []string{"DE"}}),
		},
		Default: defTargets(),
	})
	d := link.Decide(smartlink.Visit{Country: "DE"})
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
	d := link.Decide(smartlink.Visit{Country: "US"})
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
	if d := link.Decide(smartlink.Visit{}); d.Rule != "maintenance" {
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
	if d := link.Decide(smartlink.Visit{Country: "DE", Device: "mobile"}); d.Rule != "de-mobile" {
		t.Fatalf("both matchers true: Rule = %q, want de-mobile", d.Rule)
	}
	if d := link.Decide(smartlink.Visit{Country: "DE", Device: "desktop"}); d.Rule != "" {
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
		if d := link.Decide(smartlink.Visit{Country: country}); d.Rule != "geo" {
			t.Fatalf("country %q: Rule = %q, want geo", country, d.Rule)
		}
	}
	if d := link.Decide(smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("empty country matched geo rule")
	}
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
		{"en", "", false},
	}
	for _, tc := range cases {
		link := mustCompile(t, smartlink.Spec{
			Rules:   []smartlink.Rule{rule("loc", "https://hit.com", smartlink.Locale{Locales: []string{tc.rule}})},
			Default: defTargets(),
		})
		got := link.Decide(smartlink.Visit{Locale: tc.visit}).Rule == "loc"
		if got != tc.want {
			t.Errorf("rule %q vs visit %q: matched = %v, want %v", tc.rule, tc.visit, got, tc.want)
		}
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
	if d := link.Decide(smartlink.Visit{Params: map[string]string{"source": "ig"}}); d.Rule != "src" {
		t.Fatalf("listed value: Rule = %q, want src", d.Rule)
	}
	if d := link.Decide(smartlink.Visit{Params: map[string]string{"source": "FB"}}); d.Rule != "" {
		t.Fatalf("param match must be case-sensitive")
	}
	if d := link.Decide(smartlink.Visit{}); d.Rule != "" {
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

	if d := link.Decide(smartlink.Visit{}); d.Rule != "window" {
		t.Fatalf("inside window: Rule = %q, want window", d.Rule)
	}
	mock.Set(until) // Until is exclusive
	if d := link.Decide(smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("at Until: Rule = %q, want default", d.Rule)
	}
	mock.Set(from.Add(-time.Second))
	if d := link.Decide(smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("before From: Rule = %q, want default", d.Rule)
	}
	// Visit.At overrides the clock (click-log replay).
	if d := link.Decide(smartlink.Visit{At: from.Add(time.Minute)}); d.Rule != "window" {
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
	if d := link.Decide(smartlink.Visit{At: at.Add(-time.Hour)}); d.Rule != "until-only" {
		t.Fatalf("before at: Rule = %q, want until-only", d.Rule)
	}
	if d := link.Decide(smartlink.Visit{At: at.Add(time.Hour)}); d.Rule != "from-only" {
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
	first := link.Decide(smartlink.Visit{StickyKey: "visitor-1"}).Rule
	for range 10 {
		if got := link.Decide(smartlink.Visit{StickyKey: "visitor-1"}).Rule; got != first {
			t.Fatalf("same sticky key flipped: %q then %q", first, got)
		}
	}
	// Empty sticky key fails closed.
	if d := link.Decide(smartlink.Visit{}); d.Rule != "" {
		t.Fatalf("empty sticky key matched Percent")
	}
	// Distribution over many keys approximates the share.
	matched := 0
	const n = 5000
	for i := range n {
		if link.Decide(smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)}).Rule == "pct" {
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
	first := link.Decide(smartlink.Visit{StickyKey: "visitor-1"}).URL
	for range 10 {
		if got := link.Decide(smartlink.Visit{StickyKey: "visitor-1"}).URL; got != first {
			t.Fatalf("same sticky key flipped targets: %q then %q", first, got)
		}
	}
	// Empty sticky key deterministically takes the first target.
	if d := link.Decide(smartlink.Visit{}); d.URL != "https://a.com" {
		t.Fatalf("empty sticky key got %q, want first target", d.URL)
	}
	// Distribution approximates the weights.
	a := 0
	const n = 5000
	for i := range n {
		if link.Decide(smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)}).URL == "https://a.com" {
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
	d := link.Decide(smartlink.Visit{
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
	if got := link.Decide(smartlink.Visit{}).URL; got != "https://a.com//land?d=&l=&s=" {
		t.Fatalf("sparse visit URL = %q", got)
	}
}

func TestMacroPathEscaping(t *testing.T) {
	t.Parallel()
	link := mustCompile(t, smartlink.Spec{
		Default: []smartlink.Target{{URL: "https://a.com/{param.slug}/x"}},
	})
	d := link.Decide(smartlink.Visit{Params: map[string]string{"slug": "a b/c?d"}})
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
	if got := link.Decide(smartlink.Visit{Params: map[string]string{"region": "eu-1"}}).URL; got != "https://cdn-eu-1.example.com/lp" {
		t.Fatalf("URL = %q", got)
	}
	// Hostile values cannot introduce userinfo, a port, or end the authority:
	// the rendered URL still parses with a host under example.com.
	for _, hostile := range []string{"evil.com@real.com", "a:9999", "evil.com/x", "e?y", "e#z"} {
		got := link.Decide(smartlink.Visit{Params: map[string]string{"region": hostile}}).URL
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
	got := link.Decide(smartlink.Visit{Params: map[string]string{"p": "https://evil.com"}}).URL
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
	if got := drop.Decide(visit).URL; got != "https://a.com/lp?keep=orig" {
		t.Fatalf("ParamsDrop URL = %q", got)
	}

	fill := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsFill})
	if got := fill.Decide(visit).URL; got != "https://a.com/lp?extra=1&keep=orig" {
		t.Fatalf("ParamsFill URL = %q", got)
	}

	override := mustCompile(t, smartlink.Spec{Default: target, Params: smartlink.ParamsOverride})
	if got := override.Decide(visit).URL; got != "https://a.com/lp?extra=1&keep=new" {
		t.Fatalf("ParamsOverride URL = %q", got)
	}

	// No visit params: merge policies leave the URL untouched.
	if got := fill.Decide(smartlink.Visit{}).URL; got != "https://a.com/lp?keep=orig" {
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
		d := link.Decide(smartlink.Visit{StickyKey: "k" + strconv.Itoa(i)})
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
				link.Decide(smartlink.Visit{Country: "DE", StickyKey: strconv.Itoa(g*1000 + i)})
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
	d := link.Decide(smartlink.Visit{Country: "DE"})
	if d.Target.URL != tpl {
		t.Fatalf("Target.URL = %q, want raw template", d.Target.URL)
	}
	if !strings.Contains(d.URL, "/DE") {
		t.Fatalf("URL = %q, want rendered", d.URL)
	}
}
