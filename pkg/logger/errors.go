package logger

import "errors"

// ErrSentryFlushTimeout is returned by a SentryCloser when buffered Sentry events could not
// be flushed before the supplied timeout elapsed; some events may not have been delivered.
var ErrSentryFlushTimeout = errors.New("logger: sentry flush timed out")
