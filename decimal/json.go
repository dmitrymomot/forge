package decimal

// MarshalJSON implements json.Marshaler, emitting the decimal as a JSON string
// (e.g. "19.99") in the same scale-preserving form as String. A quoted string
// is used deliberately — never a bare JSON number — so no precision is lost to a
// float-based JSON number parser on the receiving end.
func (d Decimal) MarshalJSON() ([]byte, error) {
	s := d.String()
	buf := make([]byte, 0, len(s)+2)
	buf = append(buf, '"')
	buf = append(buf, s...)
	buf = append(buf, '"')
	return buf, nil
}

// UnmarshalJSON implements json.Unmarshaler. It accepts either a JSON string
// ("19.99") or a bare JSON number (19.99), parsing both with Parse; a JSON null
// leaves the receiver unchanged. Scientific notation is rejected as ErrSyntax,
// matching Parse. The value is never routed through float64, so exactness holds.
func (d *Decimal) UnmarshalJSON(p []byte) error {
	s := string(p)
	if s == "null" {
		return nil
	}
	// Strip one layer of surrounding double quotes, if present, to accept both
	// the quoted-string form we emit and a bare JSON number.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
