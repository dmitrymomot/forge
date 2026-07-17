package tenant_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/data/tenant"
)

// FuzzSubdomain asserts the resolver never panics or errors and that any
// resolved label is a single dot-free DNS label sitting directly in front of
// the base domain. The echo lookup returns the label itself so the
// properties check pure extraction.
func FuzzSubdomain(f *testing.F) {
	f.Add("acme.app.example.com")
	f.Add("app.example.com")
	f.Add("a.b.app.example.com")
	f.Add("ACME.App.Example.COM:8443")
	f.Add("[::1]:8080")
	f.Add("acme.app.example.com.")
	f.Add(".app.example.com")
	f.Add("xn--bcher-kva.app.example.com")

	echo := subdomainLookupFunc(func(_ context.Context, subdomain string) (string, error) {
		return subdomain, nil
	})
	resolve := tenant.Subdomain("app.example.com", echo)
	f.Fuzz(func(t *testing.T, host string) {
		r := newRequest("example.com", "/")
		r.Host = host
		id, err := resolve(r)
		if err != nil {
			t.Fatalf("Subdomain returned error for host %q: %v", host, err)
		}
		if id == "" {
			return
		}
		if strings.ContainsRune(id, '.') {
			t.Fatalf("resolved label %q contains a dot (host %q)", id, host)
		}
		if !strings.Contains(strings.ToLower(host), id+".app.example.com") {
			t.Fatalf("resolved label %q not found in host %q", id, host)
		}
	})
}

// FuzzPathPrefix asserts the resolver never panics or errors and that any
// resolved segment is slash-free and present in the path.
func FuzzPathPrefix(f *testing.F) {
	f.Add("/t/acme/dashboard")
	f.Add("/t/acme")
	f.Add("/t/")
	f.Add("/t")
	f.Add("/team/acme")
	f.Add("/")
	f.Add("")
	f.Add("/t//double")

	resolve := tenant.PathPrefix("/t")
	f.Fuzz(func(t *testing.T, path string) {
		r := newRequest("example.com", "/")
		r.URL.Path = path
		id, err := resolve(r)
		if err != nil {
			t.Fatalf("PathPrefix returned error for path %q: %v", path, err)
		}
		if id == "" {
			return
		}
		if strings.ContainsRune(id, '/') {
			t.Fatalf("resolved segment %q contains a slash (path %q)", id, path)
		}
		if !strings.HasPrefix(path, "/t/"+id) {
			t.Fatalf("resolved segment %q is not the segment after /t in %q", id, path)
		}
	})
}
