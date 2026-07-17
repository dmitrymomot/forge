package smartlink_test

import (
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
