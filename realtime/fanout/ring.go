package fanout

// ring is a fixed-capacity overwrite buffer of the newest messages on one
// topic. All access is serialized by the owning topicState's mutex.
type ring struct {
	buf  []Message
	next int
	size int
}

func newRing(n int) *ring {
	return &ring{buf: make([]Message, n)}
}

func (r *ring) push(m Message) {
	r.buf[r.next] = m
	r.next++
	if r.next == len(r.buf) {
		r.next = 0
	}
	if r.size < len(r.buf) {
		r.size++
	}
}

// since appends the buffered messages with ID greater than after to dst in
// ascending ID order.
func (r *ring) since(after uint64, dst []Message) []Message {
	start := r.next - r.size
	if start < 0 {
		start += len(r.buf)
	}
	for i := range r.size {
		m := r.buf[(start+i)%len(r.buf)]
		if m.ID > after {
			dst = append(dst, m)
		}
	}
	return dst
}
