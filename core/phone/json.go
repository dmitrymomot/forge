package phone

import "encoding/json"

// MarshalJSON emits the E.164 string, e.g. "+14155552671". The zero Phone emits
// JSON null.
func (p Phone) MarshalJSON() ([]byte, error) {
	if p.e164 == "" {
		return []byte("null"), nil
	}
	return json.Marshal(p.e164)
}

// UnmarshalJSON parses the E.164 string form. JSON null or an empty string sets
// the zero Phone; any other value is validated by Parse.
func (p *Phone) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*p = Phone{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		*p = Phone{}
		return nil
	}
	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*p = parsed
	return nil
}
