package eventbus

import (
	"github.com/dmitrymomot/forge/async/queue"
)

// NewService builds the worker that drains a durable bus: a queue.Service
// over the bus's broker with one equal-weight queue per subscription (slow
// subscriptions only delay themselves) and every handler registered. Run it
// under ops/supervisor next to any job-queue Service — the default service
// name is "eventbus".
//
// opts pass through to queue.NewService: concurrency, logger, config,
// queue.WithScopeContext for tenancy restore, and a name override all work as
// on a plain queue worker. Do not pass queue.WithQueues — the bus owns the
// queue set, and NewService overrides it.
//
// The service snapshots the bus's subscriptions at construction: Subscribe
// everything first, then build the service.
//
// Returns ErrNotDurable on a sync bus and ErrNoSubscriptions when nothing was
// subscribed.
func NewService(bus *Bus, opts ...queue.ServiceOption) (*queue.Service, error) {
	if bus.broker == nil {
		return nil, ErrNotDurable
	}
	weights := make(map[string]int, len(bus.names))
	for name := range bus.names {
		weights[name] = 1
	}
	if len(weights) == 0 {
		return nil, ErrNoSubscriptions
	}
	svcOpts := make([]queue.ServiceOption, 0, len(opts)+2)
	svcOpts = append(svcOpts, queue.WithName("eventbus"))
	svcOpts = append(svcOpts, opts...)
	svcOpts = append(svcOpts, queue.WithQueues(weights))
	svc, err := queue.NewService(bus.broker, svcOpts...)
	if err != nil {
		return nil, err
	}
	for _, subs := range bus.subs {
		for _, s := range subs {
			queue.Register(svc, queue.NewKind[envelope](s.name), s.handle, s.hopts...)
		}
	}
	return svc, nil
}
