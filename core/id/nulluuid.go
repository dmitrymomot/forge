package id

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// NullUUID is a UUID that may be SQL NULL. It exists because a nullable uuid column
// needs one concrete named type that codegen (sqlc's `go_type` override) can point
// at; the generic core/null.Null cannot be named in that position.
type NullUUID struct {
	UUID  UUID
	Valid bool
}

var (
	_ driver.Valuer  = NullUUID{}
	_ sql.Scanner    = (*NullUUID)(nil)
	_ json.Marshaler = NullUUID{}
)

// NullOf returns a valid NullUUID carrying u.
func NullOf(u UUID) NullUUID { return NullUUID{UUID: u, Valid: true} }

// Get returns the UUID and whether it is valid (non-NULL).
func (n NullUUID) Get() (UUID, bool) { return n.UUID, n.Valid }

// Ptr returns a pointer to a copy of the UUID, or nil when NULL.
func (n NullUUID) Ptr() *UUID {
	if !n.Valid {
		return nil
	}
	u := n.UUID
	return &u
}

// NullFromPtr returns an invalid NullUUID when p is nil, otherwise NullOf(*p).
func NullFromPtr(p *UUID) NullUUID {
	if p == nil {
		return NullUUID{}
	}
	return NullOf(*p)
}

// Value implements driver.Valuer, returning nil for NULL and the canonical string
// otherwise, matching UUID.Value.
func (n NullUUID) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.UUID.Value()
}

// Scan implements sql.Scanner. A nil source (SQL NULL) yields an invalid NullUUID;
// every other source is delegated to UUID.Scan.
func (n *NullUUID) Scan(src any) error {
	if src == nil {
		*n = NullUUID{}
		return nil
	}
	if err := n.UUID.Scan(src); err != nil {
		n.Valid = false
		return err
	}
	n.Valid = true
	return nil
}

// MarshalJSON encodes the canonical string, or JSON null when NULL.
func (n NullUUID) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.UUID.String())
}

// UnmarshalJSON decodes JSON null as an invalid NullUUID; any other value must be a
// canonical UUID string.
func (n *NullUUID) UnmarshalJSON(b []byte) error {
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		*n = NullUUID{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("id: bad NullUUID JSON: %w", ErrMalformed)
	}
	u, err := ParseUUID(s)
	if err != nil {
		return err
	}
	*n = NullOf(u)
	return nil
}
