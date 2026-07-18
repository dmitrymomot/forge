package tenant_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/web/tenant"
)

// FuzzSubdomain asserts the source never panics and that any extracted label
// is a single dot-free DNS label sitting directly in front of the base
// domain, tagged KindSubdomain.
func FuzzSubdomain(f *testing.F) {
	f.Add("acme.app.example.com")
	f.Add("app.example.com")
	f.Add("a.b.app.example.com")
	f.Add("ACME.App.Example.COM:8443")
	f.Add("[::1]:8080")
	f.Add("acme.app.example.com.")
	f.Add(".app.example.com")
	f.Add("xn--bcher-kva.app.example.com")

	extract := tenant.Subdomain("app.example.com")
	f.Fuzz(func(t *testing.T, host string) {
		r := newRequest("example.com", "/")
		r.Host = host
		ident, ok := extract(r)
		if !ok {
			if ident != (tenant.Identifier{}) {
				t.Fatalf("not-present extraction returned non-zero identifier %+v (host %q)", ident, host)
			}
			return
		}
		if ident.Kind != tenant.KindSubdomain || ident.Value == "" {
			t.Fatalf("extracted identifier %+v malformed (host %q)", ident, host)
		}
		if strings.ContainsRune(ident.Value, '.') {
			t.Fatalf("extracted label %q contains a dot (host %q)", ident.Value, host)
		}
		if !strings.Contains(strings.ToLower(host), ident.Value+".app.example.com") {
			t.Fatalf("extracted label %q not found in host %q", ident.Value, host)
		}
	})
}

// FuzzQuery asserts the RawQuery scan never panics and agrees with
// r.URL.Query() whenever net/url parses the query cleanly (the scan is
// deliberately more lenient on inputs net/url rejects wholesale).
func FuzzQuery(f *testing.F) {
	f.Add("tenant=t_123")
	f.Add("a=1&tenant=t_123")
	f.Add("tenant=%74_123")
	f.Add("%74enant=x")
	f.Add("tenant=a;b=c")
	f.Add("tenant=%zz")
	f.Add("tenant=+x")
	f.Add("")

	extract := tenant.Query("tenant")
	f.Fuzz(func(t *testing.T, rawQuery string) {
		r := newRequest("example.com", "/")
		r.URL.RawQuery = rawQuery
		ident, ok := extract(r)
		if ok && (ident.Kind != tenant.KindID || ident.Value == "") {
			t.Fatalf("extracted identifier %+v malformed (query %q)", ident, rawQuery)
		}
		vals, err := url.ParseQuery(rawQuery)
		if err != nil {
			return // net/url rejects the whole string; the lenient scan may still extract
		}
		if want := vals.Get("tenant"); want != "" {
			if !ok || ident.Value != want {
				t.Fatalf("scan got (%q, %v), url.Values got %q (query %q)", ident.Value, ok, want, rawQuery)
			}
		}
	})
}

// FuzzPathPrefix asserts the source never panics and that any extracted
// segment is slash-free, present in the path, and tagged KindPath.
func FuzzPathPrefix(f *testing.F) {
	f.Add("/t/acme/dashboard")
	f.Add("/t/acme")
	f.Add("/t/")
	f.Add("/t")
	f.Add("/team/acme")
	f.Add("/")
	f.Add("")
	f.Add("/t//double")

	extract := tenant.PathPrefix("/t")
	f.Fuzz(func(t *testing.T, path string) {
		r := newRequest("example.com", "/")
		r.URL.Path = path
		ident, ok := extract(r)
		if !ok {
			return
		}
		if ident.Kind != tenant.KindPath || ident.Value == "" {
			t.Fatalf("extracted identifier %+v malformed (path %q)", ident, path)
		}
		if strings.ContainsRune(ident.Value, '/') {
			t.Fatalf("extracted segment %q contains a slash (path %q)", ident.Value, path)
		}
		if !strings.HasPrefix(path, "/t/"+ident.Value) {
			t.Fatalf("extracted segment %q is not the segment after /t in %q", ident.Value, path)
		}
	})
}
