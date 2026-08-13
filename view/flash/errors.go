package flash

import "errors"

var (
	// ErrInvalidConfig is returned (joined) by a constructor when an option value is
	// invalid or a required dependency is nil.
	ErrInvalidConfig = errors.New("flash: invalid configuration")
	// ErrTooLarge is returned by CookieStore.Set when the encoded messages exceed
	// MaxCookieBytes. A browser would drop a cookie that size, so the write is
	// refused rather than lost silently.
	ErrTooLarge = errors.New("flash: payload too large for a cookie")
	// ErrStore is returned when the backing cache.Store fails.
	ErrStore = errors.New("flash: store failed")
)

func joinErrs(errs []error) error { return errors.Join(errs...) }
