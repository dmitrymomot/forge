// Package smartlink is the destination-decision engine of a TDS/smartlink:
// ordered rules of typed matchers ([Geo], [Device], [Locale], [ParamEquals],
// [TimeWindow], [Percent]) evaluated over a caller-built [Visit], first match
// wins, with a mandatory default target. It selects outbound destinations —
// it is not web/hostrouter (inbound hosts) and not ops/featureflag ("is X on
// for subject").
//
// [Compile] validates a consumer-hydrated [Spec] fail-fast and returns an
// immutable [Compiled]; [Compiled.Decide] is the infallible per-click hot path. Rule
// values are consumer data hydrated into the typed vocabulary — there is no
// DSL, and rule storage/admin, target health checks, and bot filtering stay
// consumer-side. The package never imports net/http: the caller builds the
// Visit from its own request facts (web/clientip + web/geoip for country,
// web/useragent for device) and emits the returned [Decision] as the click
// event.
//
// Weighted splits and [Percent] shares bucket deterministically by FNV-1a
// hash of the rule name and Visit.StickyKey — never RNG — so a visitor
// always lands on the same side. Target URLs are templates over a fixed
// macro vocabulary ({country}, {device}, {locale}, {param.NAME}) parsed at
// compile time: an unknown macro is a construction error, never an empty
// substitution at decide time. Macro values escape positionally (authority
// vs path vs query), so they can never alter the URL structure, and
// [ParamPolicy] controls merging Visit.Params into the final URL.
//
// Multi-tenant apps hydrate per-tenant rule sets and compile per-tenant
// [Compiled] values — tenancy is a passed value; there is no stored state to scope.
package smartlink

import "fmt"

// Example compiles a link that sends German mobile traffic into a weighted
// split and everyone else to the default, then decides one visit.
func Example() {
	link, err := Compile(Spec{
		Rules: []Rule{{
			Name: "de-mobile",
			When: []Matcher{
				Geo{Countries: []string{"DE", "AT", "CH"}},
				Device{Devices: []string{"mobile", "tablet"}},
			},
			Targets: []Target{
				{URL: "https://a.example.com/lp?geo={country}&click={param.click_id}", Weight: 70},
				{URL: "https://b.example.com/lp?geo={country}&click={param.click_id}", Weight: 30},
			},
		}},
		Default: []Target{{URL: "https://example.com/"}},
	})
	if err != nil {
		panic(err)
	}

	d := link.Decide(Visit{
		Country:   "de",
		Device:    "mobile",
		StickyKey: "visitor-42",
		Params:    map[string]string{"click_id": "abc123"},
	})
	fmt.Println(d.Rule)
	fmt.Println(d.URL)
	// Output:
	// de-mobile
	// https://a.example.com/lp?geo=de&click=abc123
}
