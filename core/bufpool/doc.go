// Package bufpool provides one shared, size-capped sync.Pool of *bytes.Buffer
// so renderers and encoders stop each defining a private getBuf/putBuf. It is
// deliberately zero-config: the retained-capacity cap is a tuned constant,
// which is what distinguishes it from a generic pool[T].
//
// # Usage
//
//	err := bufpool.Do(func(buf *bytes.Buffer) error {
//		buf.WriteString("hello world")
//		return nil
//	})
package bufpool
