package forgetest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponse_RequireStatus(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when status code matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusOK
		resp := &Response{rec: rec, t: t}

		resp.RequireStatus(t, http.StatusOK)
	})

	t.Run("succeeds with different status codes", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			code int
			name string
		}{
			{http.StatusCreated, "201 Created"},
			{http.StatusAccepted, "202 Accepted"},
			{http.StatusMovedPermanently, "301 Moved Permanently"},
			{http.StatusFound, "302 Found"},
			{http.StatusBadRequest, "400 Bad Request"},
			{http.StatusUnauthorized, "401 Unauthorized"},
			{http.StatusNotFound, "404 Not Found"},
			{http.StatusInternalServerError, "500 Internal Server Error"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec := httptest.NewRecorder()
				rec.Code = tt.code
				resp := &Response{rec: rec, t: t}

				resp.RequireStatus(t, tt.code)
			})
		}
	})

	t.Run("fails when status code does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusOK
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireStatus(mt, http.StatusNotFound)
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, "expected status 404, got 200")
	})
}

func TestResponse_RequireRedirect(t *testing.T) {
	t.Parallel()

	t.Run("succeeds with Location header", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusFound
		rec.Header().Set("Location", "/dashboard")
		resp := &Response{rec: rec, t: t}

		resp.RequireRedirect(t, http.StatusFound, "/dashboard")
	})

	t.Run("succeeds with HX-Redirect header when Location is missing", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusOK
		rec.Header().Set("HX-Redirect", "/dashboard")
		resp := &Response{rec: rec, t: t}

		resp.RequireRedirect(t, http.StatusOK, "/dashboard")
	})

	t.Run("prefers Location header over HX-Redirect", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusFound
		rec.Header().Set("Location", "/home")
		rec.Header().Set("HX-Redirect", "/dashboard")
		resp := &Response{rec: rec, t: t}

		resp.RequireRedirect(t, http.StatusFound, "/home")
	})

	t.Run("succeeds with different redirect codes", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			code int
			name string
		}{
			{http.StatusMovedPermanently, "301"},
			{http.StatusFound, "302"},
			{http.StatusSeeOther, "303"},
			{http.StatusTemporaryRedirect, "307"},
			{http.StatusPermanentRedirect, "308"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec := httptest.NewRecorder()
				rec.Code = tt.code
				rec.Header().Set("Location", "/target")
				resp := &Response{rec: rec, t: t}

				resp.RequireRedirect(t, tt.code, "/target")
			})
		}
	})

	t.Run("fails when status code does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusOK
		rec.Header().Set("Location", "/dashboard")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireRedirect(mt, http.StatusFound, "/dashboard")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, "expected status 302, got 200")
	})

	t.Run("fails when URL does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusFound
		rec.Header().Set("Location", "/dashboard")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireRedirect(mt, http.StatusFound, "/home")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected redirect to "/home", got "/dashboard"`)
	})

	t.Run("fails when no redirect header present", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusFound
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireRedirect(mt, http.StatusFound, "/home")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected redirect to "/home", got ""`)
	})
}

func TestResponse_RequireHeader(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when header matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		resp := &Response{rec: rec, t: t}

		resp.RequireHeader(t, "Content-Type", "application/json")
	})

	t.Run("succeeds with multiple headers", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "text/html")
		rec.Header().Set("X-Custom-Header", "custom-value")
		rec.Header().Set("Cache-Control", "no-cache")
		resp := &Response{rec: rec, t: t}

		resp.RequireHeader(t, "Content-Type", "text/html")
		resp.RequireHeader(t, "X-Custom-Header", "custom-value")
		resp.RequireHeader(t, "Cache-Control", "no-cache")
	})

	t.Run("header keys are case-insensitive", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		resp := &Response{rec: rec, t: t}

		resp.RequireHeader(t, "content-type", "application/json")
		resp.RequireHeader(t, "CONTENT-TYPE", "application/json")
	})

	t.Run("fails when header value does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHeader(mt, "Content-Type", "text/html")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "Content-Type" to be "text/html", got "application/json"`)
	})

	t.Run("fails when header is missing", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHeader(mt, "Content-Type", "application/json")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "Content-Type" to be "application/json", got ""`)
	})
}

func TestResponse_RequireHTMXTrigger(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when event is in trigger", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Trigger", "myEvent")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXTrigger(t, "myEvent")
	})

	t.Run("succeeds when event is part of JSON trigger", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Trigger", `{"event1": null, "event2": {"key": "value"}}`)
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXTrigger(t, "event1")
		resp.RequireHTMXTrigger(t, "event2")
	})

	t.Run("succeeds when event is in comma-separated list", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Trigger", "event1, event2, event3")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXTrigger(t, "event1")
		resp.RequireHTMXTrigger(t, "event2")
		resp.RequireHTMXTrigger(t, "event3")
	})

	t.Run("fails when event is not in trigger", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Trigger", "otherEvent")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXTrigger(mt, "myEvent")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected HX-Trigger to contain "myEvent", got "otherEvent"`)
	})

	t.Run("fails when trigger header is missing", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXTrigger(mt, "myEvent")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected HX-Trigger to contain "myEvent", got ""`)
	})
}

func TestResponse_RequireHTMXRetarget(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when retarget matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Retarget", "#new-target")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXRetarget(t, "#new-target")
	})

	t.Run("fails when retarget does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Retarget", "#target-a")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXRetarget(mt, "#target-b")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Retarget" to be "#target-b", got "#target-a"`)
	})
}

func TestResponse_RequireHTMXReswap(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when reswap matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Reswap", "outerHTML")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXReswap(t, "outerHTML")
	})

	t.Run("succeeds with different swap strategies", func(t *testing.T) {
		t.Parallel()
		strategies := []string{"innerHTML", "outerHTML", "beforebegin", "afterbegin", "beforeend", "afterend", "delete", "none"}

		for _, strategy := range strategies {
			t.Run(strategy, func(t *testing.T) {
				t.Parallel()
				rec := httptest.NewRecorder()
				rec.Header().Set("HX-Reswap", strategy)
				resp := &Response{rec: rec, t: t}

				resp.RequireHTMXReswap(t, strategy)
			})
		}
	})

	t.Run("fails when reswap does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Reswap", "innerHTML")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXReswap(mt, "outerHTML")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Reswap" to be "outerHTML", got "innerHTML"`)
	})
}

func TestResponse_RequireHTMXRefresh(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when refresh is true", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Refresh", "true")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXRefresh(t)
	})

	t.Run("fails when refresh is not true", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Refresh", "false")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXRefresh(mt)
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Refresh" to be "true", got "false"`)
	})

	t.Run("fails when refresh header is missing", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXRefresh(mt)
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Refresh" to be "true", got ""`)
	})
}

func TestResponse_RequireHTMXPushURL(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when push URL matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Push-Url", "/new-url")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXPushURL(t, "/new-url")
	})

	t.Run("succeeds with false value", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Push-Url", "false")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXPushURL(t, "false")
	})

	t.Run("fails when push URL does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Push-Url", "/url-a")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXPushURL(mt, "/url-b")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Push-Url" to be "/url-b", got "/url-a"`)
	})
}

func TestResponse_RequireHTMXReplaceURL(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when replace URL matches", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Replace-Url", "/replaced-url")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXReplaceURL(t, "/replaced-url")
	})

	t.Run("succeeds with false value", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Replace-Url", "false")
		resp := &Response{rec: rec, t: t}

		resp.RequireHTMXReplaceURL(t, "false")
	})

	t.Run("fails when replace URL does not match", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("HX-Replace-Url", "/url-a")
		resp := &Response{rec: rec, t: t}

		mt := expectFailure(t, func(mt *mockT) {
			resp.RequireHTMXReplaceURL(mt, "/url-b")
		})

		require.True(t, mt.failed, "expected test to fail")
		require.Contains(t, mt.message, `expected header "HX-Replace-Url" to be "/url-b", got "/url-a"`)
	})
}

func TestResponse_Body(t *testing.T) {
	t.Parallel()

	t.Run("returns response body as string", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString("Hello, World!")
		resp := &Response{rec: rec, t: t}

		body := resp.Body()
		require.Equal(t, "Hello, World!", body)
	})

	t.Run("returns empty string for empty body", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		body := resp.Body()
		require.Equal(t, "", body)
	})

	t.Run("returns multiline body", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString("Line 1\nLine 2\nLine 3")
		resp := &Response{rec: rec, t: t}

		body := resp.Body()
		require.Equal(t, "Line 1\nLine 2\nLine 3", body)
	})

	t.Run("returns JSON body", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString(`{"key": "value", "number": 42}`)
		resp := &Response{rec: rec, t: t}

		body := resp.Body()
		require.Equal(t, `{"key": "value", "number": 42}`, body)
	})
}

func TestResponse_HTML(t *testing.T) {
	t.Parallel()

	t.Run("returns parsed document", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString("<html><body><h1>Title</h1></body></html>")
		resp := &Response{rec: rec, t: t}

		doc := resp.HTML()
		require.NotNil(t, doc)
		doc.RequireText(t, "h1", "Title")
	})

	t.Run("can perform multiple assertions on document", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		_, _ = rec.WriteString(`
			<html><body>
				<h1 class="title">Welcome</h1>
				<div class="item">Item 1</div>
				<div class="item">Item 2</div>
			</body></html>
		`)
		resp := &Response{rec: rec, t: t}

		doc := resp.HTML()
		doc.RequireText(t, ".title", "Welcome")
		doc.RequireCount(t, ".item", 2)
		doc.RequireExists(t, "h1.title")
	})
}

func TestResponse_StatusCode(t *testing.T) {
	t.Parallel()

	t.Run("returns status code", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Code = http.StatusOK
		resp := &Response{rec: rec, t: t}

		code := resp.StatusCode()
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("returns different status codes", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			code int
			name string
		}{
			{http.StatusOK, "200"},
			{http.StatusCreated, "201"},
			{http.StatusNotFound, "404"},
			{http.StatusInternalServerError, "500"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				rec := httptest.NewRecorder()
				rec.Code = tt.code
				resp := &Response{rec: rec, t: t}

				code := resp.StatusCode()
				require.Equal(t, tt.code, code)
			})
		}
	})

	t.Run("returns default status code 200", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		code := resp.StatusCode()
		require.Equal(t, http.StatusOK, code)
	})
}

func TestResponse_Header(t *testing.T) {
	t.Parallel()

	t.Run("returns header value", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		resp := &Response{rec: rec, t: t}

		value := resp.Header("Content-Type")
		require.Equal(t, "application/json", value)
	})

	t.Run("returns empty string for missing header", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		value := resp.Header("X-Missing-Header")
		require.Equal(t, "", value)
	})

	t.Run("header keys are case-insensitive", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "text/html")
		resp := &Response{rec: rec, t: t}

		require.Equal(t, "text/html", resp.Header("Content-Type"))
		require.Equal(t, "text/html", resp.Header("content-type"))
		require.Equal(t, "text/html", resp.Header("CONTENT-TYPE"))
	})

	t.Run("returns first value for multi-value headers", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Add("X-Multi", "value1")
		rec.Header().Add("X-Multi", "value2")
		resp := &Response{rec: rec, t: t}

		value := resp.Header("X-Multi")
		require.Equal(t, "value1", value)
	})
}

func TestResponse_Recorder(t *testing.T) {
	t.Parallel()

	t.Run("returns underlying recorder", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		resp := &Response{rec: rec, t: t}

		recorder := resp.Recorder()
		require.Same(t, rec, recorder)
	})

	t.Run("can access recorder methods", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		rec.Header().Set("X-Custom", "value")
		_, _ = rec.WriteString("test body")
		rec.Code = http.StatusCreated
		resp := &Response{rec: rec, t: t}

		recorder := resp.Recorder()
		require.Equal(t, http.StatusCreated, recorder.Code)
		require.Equal(t, "test body", recorder.Body.String())
		require.Equal(t, "value", recorder.Header().Get("X-Custom"))
	})
}
