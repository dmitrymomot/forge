package eventbus

import "errors"

var (
	// ErrScopeMissing is returned by Publish/PublishTx when a scope hook is
	// configured and yields an error or empty scope.
	ErrScopeMissing = errors.New("eventbus: scope missing")
	// ErrTxUnsupported is returned by PublishTx when the bus is sync or its
	// broker does not implement queue.TxPusher.
	ErrTxUnsupported = errors.New("eventbus: transactional publish unsupported")
	// ErrNotDurable is returned by NewService on a bus built with NewSync:
	// sync subscriptions run inside Publish and have no worker service.
	ErrNotDurable = errors.New("eventbus: bus is not durable")
	// ErrNoSubscriptions is returned by NewService when nothing has been
	// subscribed: a worker with no queues to drain is a wiring error.
	ErrNoSubscriptions = errors.New("eventbus: no subscriptions")
)
