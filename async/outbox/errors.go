package outbox

import "errors"

// ErrInvalidConfig is returned by Config.Validate and NewRelay on bad configuration.
var ErrInvalidConfig = errors.New("outbox: invalid config")
