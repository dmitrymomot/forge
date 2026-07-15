package queue_test

import (
	"testing"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
)

var _ queue.Broker = (*queue.MemoryBroker)(nil)

func TestMemoryBroker_Conformance(t *testing.T) {
	t.Parallel()
	brokertest.Run(t, func(t *testing.T) queue.Broker { return queue.NewMemoryBroker() })
}
