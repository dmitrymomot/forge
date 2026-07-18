package smartlink_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// Example compiles a link that sends German mobile traffic into a weighted
// split and everyone else to the default, then decides one visit.
func Example() {
	link, err := smartlink.Compile(smartlink.Spec{
		Rules: []smartlink.Rule{{
			Name: "de-mobile",
			When: []smartlink.Matcher{
				smartlink.Geo{Countries: []string{"DE", "AT", "CH"}},
				smartlink.Device{Devices: []string{"mobile", "tablet"}},
			},
			Targets: []smartlink.Target{
				{URL: "https://a.example.com/lp?geo={country}&click={param.click_id}", Weight: 70},
				{URL: "https://b.example.com/lp?geo={country}&click={param.click_id}", Weight: 30},
			},
		}},
		Default: []smartlink.Target{{URL: "https://example.com/"}},
	})
	if err != nil {
		panic(err)
	}

	d := link.Decide(smartlink.Visit{
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

// Example_manager creates a fixed-destination link with a vanity code and
// serves one hit through the redirect handler: the incoming click_id query
// param forwards into the destination under the default ParamsFill policy,
// while the target's own src param is left untouched.
func Example_manager() {
	mgr, err := smartlink.NewManager(smartlink.NewMemoryStore(), smartlink.WithBaseURL("https://s.example.com/"))
	if err != nil {
		panic(err)
	}

	link, err := mgr.Create(context.Background(), smartlink.CreateParams{
		Code:   "promo",
		Target: "https://dest.example.com/landing?src=affiliate42",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(link.ShortURL)

	mux := http.NewServeMux()
	mux.Handle("/{code}", mgr.Handler())

	req := httptest.NewRequest(http.MethodGet, "/promo?click_id=abc123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	fmt.Println(rec.Code)
	fmt.Println(rec.Header().Get("Location"))
	// Output:
	// https://s.example.com/promo
	// 302
	// https://dest.example.com/landing?src=affiliate42&click_id=abc123
}
