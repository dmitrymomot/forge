package secheaders

import "errors"

// ErrInvalidConfig marks bad Config values (unknown FrameOptions, negative HSTS).
var ErrInvalidConfig = errors.New("secheaders: invalid config")
