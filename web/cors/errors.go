package cors

import "errors"

// ErrInvalidConfig marks invalid CORS policy: malformed origin patterns or
// the wildcard-origin + credentials combination.
var ErrInvalidConfig = errors.New("cors: invalid config")
