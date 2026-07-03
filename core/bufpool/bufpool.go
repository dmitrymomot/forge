package bufpool

import (
	"bytes"
	"sync"
)

// maxCap bounds the capacity of buffers retained by the pool. Larger buffers
// are dropped on Put so a single big render cannot pin memory in the pool.
const maxCap = 64 << 10 // 64 KiB

var pool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Get returns a reset buffer from the shared pool.
func Get() *bytes.Buffer {
	b := pool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

// Put returns b to the pool. A nil buffer, or one whose capacity exceeds the
// internal cap, is dropped rather than retained.
func Put(b *bytes.Buffer) {
	if b == nil || b.Cap() > maxCap {
		return
	}
	pool.Put(b)
}

// Do borrows a buffer, passes it to fn, and returns it to the pool afterwards —
// even if fn panics. The buffer must not be retained after fn returns.
func Do(fn func(*bytes.Buffer) error) error {
	b := Get()
	defer Put(b)
	return fn(b)
}
