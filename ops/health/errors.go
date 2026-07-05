package health

import "errors"

// ErrDraining is returned by a down Gate's Check while the app is shutting down.
var ErrDraining = errors.New("health: draining")
