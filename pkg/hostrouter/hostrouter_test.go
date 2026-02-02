package hostrouter_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

func TestRouter_ExactHost(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("example"))
		}),
		"other.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("other"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{"exact match", "example.com", "example", 200},
		{"exact match other", "other.com", "other", 200},
		{"case insensitive", "Example.COM", "example", 200},
		{"with port", "example.com:8080", "example", 200},
		{"no match", "unknown.com", "404 page not found\n", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantCode, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_WildcardHost(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"*.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("wildcard"))
		}),
		"specific.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("specific"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{"specific takes priority", "specific.example.com", "specific", 200},
		{"wildcard match foo", "foo.example.com", "wildcard", 200},
		{"wildcard match bar", "bar.example.com", "wildcard", 200},
		{"wildcard case insensitive", "FOO.Example.COM", "wildcard", 200},
		{"no match - root domain", "example.com", "404 page not found\n", 404},
		{"no match - other domain", "other.com", "404 page not found\n", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantCode, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_WildcardMultiLevelSubdomains(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"*.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("wildcard"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{
			name:     "single level subdomain matches",
			host:     "foo.example.com",
			wantBody: "wildcard",
			wantCode: 200,
		},
		{
			name:     "multi level subdomain does not match",
			host:     "foo.bar.example.com",
			wantBody: "404 page not found\n",
			wantCode: 404,
		},
		{
			name:     "root domain does not match",
			host:     "example.com",
			wantBody: "404 page not found\n",
			wantCode: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantCode, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_FallbackToDefault(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"api.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("api"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("default"))
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
	}{
		{"api host", "api.example.com", "api"},
		{"other host uses fallback", "www.example.com", "default"},
		{"unknown host uses fallback", "unknown.com", "default"},
		{"empty host uses fallback", "", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_EmptyRoutes(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback"))
	})

	router := hostrouter.New(routes, fallback)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, "fallback", rec.Body.String(), "unexpected response body")
}

func TestRouter_PatternNormalization(t *testing.T) {
	t.Parallel()

	t.Run("whitespace in patterns is trimmed", func(t *testing.T) {
		t.Parallel()

		routes := hostrouter.Routes{
			"  example.com  ": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("trimmed"))
			}),
		}

		fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})

		router := hostrouter.New(routes, fallback)

		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "example.com"
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, 200, rec.Code, "should match trimmed pattern")
		require.Equal(t, "trimmed", rec.Body.String())
	})

	t.Run("empty patterns are ignored", func(t *testing.T) {
		t.Parallel()

		routes := hostrouter.Routes{
			"":              http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			"   ":           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			"example.com":   http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			"*.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		}

		fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})

		// Should not panic
		router := hostrouter.New(routes, fallback)
		require.NotNil(t, router)
	})

	t.Run("wildcard pattern normalization", func(t *testing.T) {
		t.Parallel()

		routes := hostrouter.Routes{
			"  *.example.com  ": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("wildcard"))
			}),
		}

		fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})

		router := hostrouter.New(routes, fallback)

		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "foo.example.com"
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, 200, rec.Code, "should match trimmed wildcard pattern")
		require.Equal(t, "wildcard", rec.Body.String())
	})
}

func TestRouter_IPv6Host(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"[::1]": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ipv6"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{
			name:     "ipv6 without port",
			host:     "[::1]",
			wantBody: "ipv6",
			wantCode: 200,
		},
		{
			name:     "ipv6 with port",
			host:     "[::1]:8080",
			wantBody: "ipv6",
			wantCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantCode, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_PortStripping(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("matched"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
		wantCode int
	}{
		{
			name:     "no port",
			host:     "example.com",
			wantBody: "matched",
			wantCode: 200,
		},
		{
			name:     "standard http port",
			host:     "example.com:80",
			wantBody: "matched",
			wantCode: 200,
		},
		{
			name:     "standard https port",
			host:     "example.com:443",
			wantBody: "matched",
			wantCode: 200,
		},
		{
			name:     "custom port",
			host:     "example.com:3000",
			wantBody: "matched",
			wantCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tt.wantCode, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}

func TestRouter_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("example"))
		}),
		"*.wildcard.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("wildcard"))
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback"))
	})

	router := hostrouter.New(routes, fallback)

	// Simulate concurrent requests
	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	hosts := []string{
		"example.com",
		"foo.wildcard.com",
		"bar.wildcard.com",
		"unknown.com",
	}

	for i := range numGoroutines {
		go func(idx int) {
			defer wg.Done()

			host := hosts[idx%len(hosts)]
			req := httptest.NewRequest("GET", "/", nil)
			req.Host = host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			// Verify we got a response (no panic)
			require.NotEmpty(t, rec.Body.String(), "should receive a response")
		}(i)
	}

	wg.Wait()
}

func TestRouter_HandlerPanicPropagation(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"panic.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("intentional panic")
		}),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fallback"))
	})

	router := hostrouter.New(routes, fallback)

	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "panic.com"
	rec := httptest.NewRecorder()

	// Verify panic propagates (standard Go HTTP behavior)
	require.Panics(t, func() {
		router.ServeHTTP(rec, req)
	}, "handler panic should propagate")
}

func TestRouter_CaseSensitivityAndPriority(t *testing.T) {
	t.Parallel()

	routes := hostrouter.Routes{
		"Example.COM":          http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("exact")) }),
		"*.Example.COM":        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("wildcard")) }),
		"specific.example.com": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("specific")) }),
	}

	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	router := hostrouter.New(routes, fallback)

	tests := []struct {
		name     string
		host     string
		wantBody string
	}{
		{
			name:     "exact match case insensitive",
			host:     "example.com",
			wantBody: "exact",
		},
		{
			name:     "exact match different case",
			host:     "EXAMPLE.COM",
			wantBody: "exact",
		},
		{
			name:     "specific exact takes priority over wildcard",
			host:     "specific.example.com",
			wantBody: "specific",
		},
		{
			name:     "wildcard match",
			host:     "foo.example.com",
			wantBody: "wildcard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, 200, rec.Code, "unexpected status code")
			require.Equal(t, tt.wantBody, rec.Body.String(), "unexpected response body")
		})
	}
}
