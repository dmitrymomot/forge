package tariff

import (
	"encoding/json"

	"github.com/dmitrymomot/forge/core/decimal"
)

type scheduleJSON struct {
	Mode  Mode       `json:"mode"`
	Bands []bandJSON `json:"bands"`
}

type bandJSON struct {
	UpTo *decimal.Decimal `json:"up_to,omitempty"`
	Rate decimal.Decimal  `json:"rate"`
}

// MarshalJSON implements json.Marshaler, so deal band sets can be stored as
// data (e.g. in data/settings) and re-validated on load. Bounds and rates are
// emitted as JSON strings (decimal's exact form); the open band omits
// "up_to". Marshaling a zero Schedule returns ErrNoBands.
func (s Schedule) MarshalJSON() ([]byte, error) {
	if len(s.bands) == 0 {
		return nil, ErrNoBands
	}
	out := scheduleJSON{Mode: s.mode, Bands: make([]bandJSON, len(s.bands))}
	for i, b := range s.bands {
		bj := bandJSON{Rate: b.Rate}
		if !b.Open {
			bj.UpTo = new(b.UpTo)
		}
		out.Bands[i] = bj
	}
	return json.Marshal(out)
}

// UnmarshalJSON implements json.Unmarshaler. A band without "up_to" is the
// open band. The decoded band set passes through New, so an invalid stored
// schedule fails to load instead of silently rating wrong.
func (s *Schedule) UnmarshalJSON(p []byte) error {
	var in scheduleJSON
	if err := json.Unmarshal(p, &in); err != nil {
		return err
	}
	bands := make([]Band, len(in.Bands))
	for i, bj := range in.Bands {
		b := Band{Rate: bj.Rate, Open: bj.UpTo == nil}
		if bj.UpTo != nil {
			b.UpTo = *bj.UpTo
		}
		bands[i] = b
	}
	ns, err := New(in.Mode, bands...)
	if err != nil {
		return err
	}
	*s = ns
	return nil
}
