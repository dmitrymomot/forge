package keyset

import "errors"

// ErrNoPrimary is returned by New when no primary key was configured.
var ErrNoPrimary = errors.New("keyset: no primary key configured")

// ErrBadKeyMaterial is returned when supplied key material is malformed.
var ErrBadKeyMaterial = errors.New("keyset: invalid key material")
