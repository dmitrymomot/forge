package attribution

import "errors"

// ErrNoTouch means no valid touch is stored: the cookie is absent,
// tampered with, malformed, or outside the attribution window.
var ErrNoTouch = errors.New("attribution: no touch")
