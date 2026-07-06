package csrf

import "errors"

var (
	// ErrTokenMissing means the request carried no token echo (header or form
	// field) or no valid token cookie existed for an unsafe method.
	ErrTokenMissing = errors.New("csrf: token missing")
	// ErrTokenInvalid means the echoed token did not match the cookie token.
	ErrTokenInvalid = errors.New("csrf: token invalid")
)
