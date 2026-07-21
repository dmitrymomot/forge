package fanout

import "errors"

var (
	// ErrClosed is returned by Publish and Subscribe after Close, and reported
	// by Subscription.Err when the hub shut the subscription down.
	ErrClosed = errors.New("fanout: hub closed")
	// ErrSlowConsumer is reported by Subscription.Err when the CloseSlow
	// policy tore the subscription down because its buffer overflowed.
	ErrSlowConsumer = errors.New("fanout: slow consumer")
	// ErrInvalidTopic is returned for an empty topic or one containing the
	// reserved bytes 0x00 or 0x1F.
	ErrInvalidTopic = errors.New("fanout: invalid topic")
	// ErrNoTopics is returned by Subscribe when called with no topics.
	ErrNoTopics = errors.New("fanout: no topics")
	// ErrScopeMissing is returned by Publish and Subscribe when a scope hook
	// is configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("fanout: scope missing")
	// ErrInvalidScope is returned when the scope hook yields a scope
	// containing the reserved bytes 0x00 or 0x1F.
	ErrInvalidScope = errors.New("fanout: invalid scope")
	// ErrReplayDisabled is returned by Subscribe when WithResumeAfter is used
	// on a hub built without WithReplay.
	ErrReplayDisabled = errors.New("fanout: replay disabled")
)
