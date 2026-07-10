package jwt

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
)

// Audience is the RFC 7519 "aud" claim. It unmarshals from either a JSON
// string or an array of strings and marshals to a bare string when it
// holds exactly one value.
type Audience []string

// Contains reports whether v is one of the audience values.
func (a Audience) Contains(v string) bool {
	return slices.Contains(a, v)
}

func (a Audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

func (a *Audience) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = Audience{s}
		return nil
	}
	var ss []string
	if err := json.Unmarshal(b, &ss); err != nil {
		return err
	}
	*a = Audience(ss)
	return nil
}

// NumericDate is an RFC 7519 NumericDate: unix seconds as a JSON number.
// Fractional seconds are accepted on input and truncated.
type NumericDate struct {
	time.Time
}

// NewNumericDate returns t truncated to whole seconds.
func NewNumericDate(t time.Time) *NumericDate {
	return &NumericDate{Time: t.Truncate(time.Second)}
}

func (d NumericDate) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, d.Unix(), 10), nil
}

func (d *NumericDate) UnmarshalJSON(b []byte) error {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("jwt: invalid NumericDate %q", b)
	}
	d.Time = time.Unix(int64(f), 0)
	return nil
}

// Claims holds the RFC 7519 registered claims. Consumers embed it:
//
//	type AccessClaims struct {
//	    jwt.Claims
//	    TenantID string `json:"tid"`
//	}
type Claims struct {
	ExpiresAt *NumericDate `json:"exp,omitempty"`
	NotBefore *NumericDate `json:"nbf,omitempty"`
	IssuedAt  *NumericDate `json:"iat,omitempty"`
	Issuer    string       `json:"iss,omitempty"`
	Subject   string       `json:"sub,omitempty"`
	ID        string       `json:"jti,omitempty"`
	Audience  Audience     `json:"aud,omitempty"`
}
