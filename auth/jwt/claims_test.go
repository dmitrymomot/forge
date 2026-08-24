package jwt_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/auth/jwt"
)

func TestAudienceJSON(t *testing.T) {
	t.Parallel()

	t.Run("unmarshal string", func(t *testing.T) {
		t.Parallel()
		var a jwt.Audience
		if err := json.Unmarshal([]byte(`"api"`), &a); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(a) != 1 || a[0] != "api" {
			t.Fatalf("got %v, want [api]", a)
		}
	})

	t.Run("unmarshal array", func(t *testing.T) {
		t.Parallel()
		var a jwt.Audience
		if err := json.Unmarshal([]byte(`["api","web"]`), &a); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(a) != 2 || a[0] != "api" || a[1] != "web" {
			t.Fatalf("got %v, want [api web]", a)
		}
	})

	t.Run("unmarshal rejects number", func(t *testing.T) {
		t.Parallel()
		var a jwt.Audience
		if err := json.Unmarshal([]byte(`42`), &a); err == nil {
			t.Fatal("want error for non-string aud")
		}
	})

	t.Run("marshal single as string", func(t *testing.T) {
		t.Parallel()
		b, err := json.Marshal(jwt.Audience{"api"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != `"api"` {
			t.Fatalf("got %s, want %q", b, `"api"`)
		}
	})

	t.Run("marshal multiple as array", func(t *testing.T) {
		t.Parallel()
		b, err := json.Marshal(jwt.Audience{"api", "web"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != `["api","web"]` {
			t.Fatalf("got %s, want array", b)
		}
	})

	t.Run("contains", func(t *testing.T) {
		t.Parallel()
		a := jwt.Audience{"api", "web"}
		if !a.Contains("web") || a.Contains("mobile") {
			t.Fatalf("Contains misbehaves: %v", a)
		}
	})
}

func TestNumericDateJSON(t *testing.T) {
	t.Parallel()

	t.Run("round trip unix seconds", func(t *testing.T) {
		t.Parallel()
		d := jwt.NewNumericDate(time.Unix(1300819380, 0))
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(b) != "1300819380" {
			t.Fatalf("got %s, want 1300819380", b)
		}
		var back jwt.NumericDate
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !back.Equal(time.Unix(1300819380, 0)) {
			t.Fatalf("got %v", back.Time)
		}
	})

	t.Run("fractional seconds truncate", func(t *testing.T) {
		t.Parallel()
		var d jwt.NumericDate
		if err := json.Unmarshal([]byte("1300819380.75"), &d); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if d.Unix() != 1300819380 {
			t.Fatalf("got %d, want 1300819380", d.Unix())
		}
	})

	t.Run("rejects non-numbers", func(t *testing.T) {
		t.Parallel()
		for _, in := range []string{`"soon"`, `null`, `{}`, `NaN`} {
			var d jwt.NumericDate
			if err := json.Unmarshal([]byte(in), &d); err == nil {
				t.Fatalf("want error for %s", in)
			}
		}
	})
}

func TestClaimsEmbedding(t *testing.T) {
	t.Parallel()
	type access struct {
		jwt.Claims
		TenantID string `json:"tid"`
	}
	in := access{
		Issuer:    "https://api.example.com",
		Subject:   "user-1",
		Audience:  jwt.Audience{"my-app"},
		ExpiresAt: jwt.NewNumericDate(time.Unix(1300819380, 0)),
		ID:        "jti-1",
		TenantID:  "t-42",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out access
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Issuer != in.Issuer || out.TenantID != "t-42" || !out.Audience.Contains("my-app") {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	// omitempty: zero claims marshal to {} — no null exp/nbf noise.
	empty, err := json.Marshal(jwt.Claims{})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if string(empty) != "{}" {
		t.Fatalf("empty claims marshal to %s, want {}", empty)
	}
}
