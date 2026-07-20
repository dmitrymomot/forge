package postback_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/dmitrymomot/forge/comms/postback"
)

func mustVocab(t *testing.T, names ...string) postback.Vocabulary {
	t.Helper()
	v, err := postback.NewVocabulary(names...)
	if err != nil {
		t.Fatalf("NewVocabulary(%q): %v", names, err)
	}
	return v
}

func TestNewVocabulary(t *testing.T) {
	t.Parallel()

	t.Run("valid names", func(t *testing.T) {
		t.Parallel()
		if _, err := postback.NewVocabulary("click_id", "payout", "sub-1", "goal.id", "S2"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty vocabulary is legal", func(t *testing.T) {
		t.Parallel()
		if _, err := postback.NewVocabulary(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid names fail closed", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"", "click id", "a{b", "a}b", "päy", "a/b"} {
			if _, err := postback.NewVocabulary(name); !errors.Is(err, postback.ErrInvalidMacro) {
				t.Errorf("NewVocabulary(%q) = %v, want ErrInvalidMacro", name, err)
			}
		}
	})
}

func TestNewDestination(t *testing.T) {
	t.Parallel()
	vocab := mustVocab(t, "click_id", "payout", "sub1")

	t.Run("valid template", func(t *testing.T) {
		t.Parallel()
		d, err := postback.NewDestination("https://t.example.com/pb?cid={click_id}&sum={payout}", vocab)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Raw() != "https://t.example.com/pb?cid={click_id}&sum={payout}" {
			t.Errorf("Raw() = %q", d.Raw())
		}
		if d.Method() != http.MethodGet {
			t.Errorf("Method() = %q, want GET", d.Method())
		}
	})

	t.Run("literal template without macros", func(t *testing.T) {
		t.Parallel()
		d, err := postback.NewDestination("https://t.example.com/ping", postback.Vocabulary{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := d.Render(nil); got != "https://t.example.com/ping" {
			t.Errorf("Render() = %q", got)
		}
	})

	t.Run("unknown macro", func(t *testing.T) {
		t.Parallel()
		if _, err := postback.NewDestination("https://t.example.com/pb?cid={clickid}", vocab); !errors.Is(err, postback.ErrUnknownMacro) {
			t.Errorf("err = %v, want ErrUnknownMacro", err)
		}
	})

	t.Run("zero vocabulary rejects any macro", func(t *testing.T) {
		t.Parallel()
		if _, err := postback.NewDestination("https://t.example.com/pb?cid={click_id}", postback.Vocabulary{}); !errors.Is(err, postback.ErrUnknownMacro) {
			t.Errorf("err = %v, want ErrUnknownMacro", err)
		}
	})

	t.Run("malformed templates", func(t *testing.T) {
		t.Parallel()
		cases := map[string]string{
			"unclosed brace":      "https://t.example.com/pb?cid={click_id",
			"unmatched close":     "https://t.example.com/pb?cid=click_id}",
			"close before open":   "https://t.example.com/pb?a=}{click_id}",
			"trailing close":      "https://t.example.com/pb?cid={click_id}}",
			"relative URL":        "/pb?cid={click_id}",
			"missing host":        "https:///pb?cid={click_id}",
			"ftp scheme":          "ftp://t.example.com/pb?cid={click_id}",
			"fragment":            "https://t.example.com/pb#frag?cid={click_id}",
			"bare fragment":       "https://t.example.com/pb?cid={click_id}#",
			"macro in host":       "https://{click_id}.example.com/pb",
			"macro in port":       "https://t.example.com:{click_id}/pb",
			"macro before scheme": "{click_id}https://t.example.com/pb",
			"macro in scheme":     "http{click_id}://t.example.com/pb",
			"invalid elided URL":  "https://t.example .com/pb?cid={click_id}",
			"empty template":      "",
			"scheme-only":         "https://",
		}
		for name, raw := range cases {
			if _, err := postback.NewDestination(raw, vocab); !errors.Is(err, postback.ErrInvalidTemplate) {
				t.Errorf("%s: NewDestination(%q) = %v, want ErrInvalidTemplate", name, raw, err)
			}
		}
	})

	t.Run("method option", func(t *testing.T) {
		t.Parallel()
		d, err := postback.NewDestination("https://t.example.com/pb?cid={click_id}", vocab, postback.WithMethod("post"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Method() != http.MethodPost {
			t.Errorf("Method() = %q, want POST", d.Method())
		}
	})

	t.Run("invalid method", func(t *testing.T) {
		t.Parallel()
		for _, m := range []string{"DELETE", "PATCH", "PUT", ""} {
			if _, err := postback.NewDestination("https://t.example.com/pb", vocab, postback.WithMethod(m)); !errors.Is(err, postback.ErrInvalidMethod) {
				t.Errorf("WithMethod(%q) = %v, want ErrInvalidMethod", m, err)
			}
		}
	})
}

func TestDestinationRender(t *testing.T) {
	t.Parallel()
	vocab := mustVocab(t, "click_id", "payout", "sub1", "goal")

	tests := map[string]struct {
		raw    string
		values map[string]string
		want   string
	}{
		"query escaping": {
			raw:    "https://t.example.com/pb?cid={click_id}&sum={payout}",
			values: map[string]string{"click_id": "a b&c=d", "payout": "12.50"},
			want:   "https://t.example.com/pb?cid=a+b%26c%3Dd&sum=12.50",
		},
		"path escaping": {
			raw:    "https://t.example.com/pb/{click_id}/done",
			values: map[string]string{"click_id": "a/b:c?x"},
			want:   "https://t.example.com/pb/a%2Fb%3Ac%3Fx/done",
		},
		"absent macro renders empty": {
			raw:    "https://t.example.com/pb?cid={click_id}&s1={sub1}",
			values: map[string]string{"click_id": "abc"},
			want:   "https://t.example.com/pb?cid=abc&s1=",
		},
		"nil values render all empty": {
			raw:    "https://t.example.com/pb?cid={click_id}",
			values: nil,
			want:   "https://t.example.com/pb?cid=",
		},
		"duplicate macro renders twice": {
			raw:    "https://t.example.com/pb?cid={click_id}&again={click_id}",
			values: map[string]string{"click_id": "abc"},
			want:   "https://t.example.com/pb?cid=abc&again=abc",
		},
		"extra values are ignored": {
			raw:    "https://t.example.com/pb?cid={click_id}",
			values: map[string]string{"click_id": "abc", "goal": "reg"},
			want:   "https://t.example.com/pb?cid=abc",
		},
		"value cannot alter structure": {
			raw:    "https://t.example.com/pb?cid={click_id}",
			values: map[string]string{"click_id": "x#frag"},
			want:   "https://t.example.com/pb?cid=x%23frag",
		},
		"path dot-segment values are encoded": {
			raw:    "https://t.example.com/pb/{click_id}/done",
			values: map[string]string{"click_id": ".."},
			want:   "https://t.example.com/pb/%2E%2E/done",
		},
		"path dot value is encoded": {
			raw:    "https://t.example.com/pb/{click_id}/done",
			values: map[string]string{"click_id": "."},
			want:   "https://t.example.com/pb/%2E/done",
		},
		"multi-segment traversal cannot form": {
			raw:    "https://t.example.com/pb/{click_id}/done",
			values: map[string]string{"click_id": "../.."},
			want:   "https://t.example.com/pb/..%2F../done",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := postback.NewDestination(tc.raw, vocab)
			if err != nil {
				t.Fatalf("NewDestination(%q): %v", tc.raw, err)
			}
			if got := d.Render(tc.values); got != tc.want {
				t.Errorf("Render() = %q, want %q", got, tc.want)
			}
		})
	}
}
