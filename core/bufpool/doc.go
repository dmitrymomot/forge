// Package bufpool is one shared, size-capped sync.Pool of *bytes.Buffer so
// transactional renderers and encoders stop each defining a private getBuf/
// putBuf. It is deliberately zero-config: the retained-capacity cap is a tuned
// constant, which is what distinguishes it from a generic pool[T].
package bufpool
