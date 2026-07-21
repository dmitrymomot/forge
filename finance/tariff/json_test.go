package tariff_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/finance/tariff"
)

func TestScheduleJSONRoundTrip(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Graduated)

	p, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// The open band omits "up_to"; bounds/rates are exact JSON strings.
	if got := string(p); strings.Count(got, "up_to") != 2 {
		t.Fatalf("want exactly 2 up_to keys in %s", got)
	}
	if !strings.Contains(string(p), `"mode":"graduated"`) {
		t.Fatalf("mode missing in %s", p)
	}

	var back tariff.Schedule
	if err := json.Unmarshal(p, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Mode() != tariff.Graduated {
		t.Fatalf("Mode = %q", back.Mode())
	}
	want, err := s.Apply(d("100"))
	if err != nil {
		t.Fatalf("Apply original: %v", err)
	}
	got, err := back.Apply(d("100"))
	if err != nil {
		t.Fatalf("Apply round-tripped: %v", err)
	}
	if got.Total.Cmp(want.Total) != 0 || len(got.Lines) != len(want.Lines) {
		t.Fatalf("round-trip drift: got %s/%d lines, want %s/%d lines", got.Total, len(got.Lines), want.Total, len(want.Lines))
	}
}

func TestScheduleUnmarshalValidates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want error
	}{
		{"bad mode", `{"mode":"tiered","bands":[{"rate":"0.1"}]}`, tariff.ErrInvalidMode},
		{"no bands", `{"mode":"volume","bands":[]}`, tariff.ErrNoBands},
		{"out-of-order bounds", `{"mode":"graduated","bands":[{"up_to":"50","rate":"0.2"},{"up_to":"10","rate":"0.3"},{"rate":"0.4"}]}`, tariff.ErrBandOrder},
		{"no open band", `{"mode":"graduated","bands":[{"up_to":"10","rate":"0.2"}]}`, tariff.ErrOpenBand},
		{"negative rate", `{"mode":"volume","bands":[{"rate":"-0.1"}]}`, tariff.ErrNegativeRate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var s tariff.Schedule
			if err := json.Unmarshal([]byte(tc.in), &s); !errors.Is(err, tc.want) {
				t.Fatalf("Unmarshal = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("malformed json", func(t *testing.T) {
		t.Parallel()
		var s tariff.Schedule
		if err := json.Unmarshal([]byte(`{"mode":`), &s); err == nil {
			t.Fatal("want error for malformed JSON")
		}
	})

	t.Run("malformed decimal", func(t *testing.T) {
		t.Parallel()
		var s tariff.Schedule
		if err := json.Unmarshal([]byte(`{"mode":"volume","bands":[{"rate":"1.2.3"}]}`), &s); err == nil {
			t.Fatal("want error for malformed decimal")
		}
	})
}

func TestScheduleMarshalZero(t *testing.T) {
	t.Parallel()
	var s tariff.Schedule
	if _, err := json.Marshal(s); !errors.Is(err, tariff.ErrNoBands) {
		t.Fatalf("Marshal zero = %v, want ErrNoBands", err)
	}
}

func TestScheduleUnmarshalFailureLeavesReceiver(t *testing.T) {
	t.Parallel()
	s := catalogSchedule(t, tariff.Volume)
	if err := json.Unmarshal([]byte(`{"mode":"bogus","bands":[{"rate":"0.1"}]}`), &s); err == nil {
		t.Fatal("want error")
	}
	// A failed load must not clobber the previously valid schedule.
	if s.Mode() != tariff.Volume || len(s.Bands()) != 3 {
		t.Fatalf("failed Unmarshal clobbered receiver: mode %q, %d bands", s.Mode(), len(s.Bands()))
	}
}
