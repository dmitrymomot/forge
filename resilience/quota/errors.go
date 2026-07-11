package quota

import "errors"

// ErrInvalidCost is returned by Allow when cost is negative.
var ErrInvalidCost = errors.New("quota: cost must be non-negative")

// ErrInvalidLimit is returned when a Limit has Included < 0, or Max below
// Included while not Unlimited.
var ErrInvalidLimit = errors.New("quota: Max must be >= Included, or Unlimited")
