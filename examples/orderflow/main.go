// Command orderflow is an integration demo of every async package working
// together as one order-processing backend. A single in-memory binary, no
// external services: swap the memory constructors for durable Store/Broker
// implementations and the wiring stays identical.
//
// The pipeline, end to end:
//
//	POST /orders ──(outbox: PublishTx intent row)──> relay ──> broker
//	    └─ collector buffers request telemetry (write-behind)
//	broker ──(eventbus)──> "fulfill" subscription ──> workflow order.fulfill
//	    order.placed also routes (eventrouter) to the local /analytics endpoint
//	workflow: charge_payment -> reserve_stock -> complete
//	    out of stock => workflow.Fail => compensation refunds the charge
//	    complete publishes order.completed -> "receipt" subscription + analytics
//	scheduler ──(every 15s)──> shop.report queue job ──> summary log line
//
// The code is split by concern: shop.go is the business state and events,
// fulfillment.go the workflow and event subscriptions, web.go the HTTP layer
// and analytics egress, report.go the scheduled summary. main wires them.
//
// Run it and watch one order fulfill and one compensate:
//
//	go run ./examples/orderflow
//	curl -s localhost:8080/orders -d '{"item":"espresso","qty":1}'
//	curl -s localhost:8080/orders -d '{"item":"unicorn","qty":1}'
package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/dmitrymomot/forge/async/eventbus"
	"github.com/dmitrymomot/forge/async/outbox"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/scheduler"
	"github.com/dmitrymomot/forge/async/workflow"
	"github.com/dmitrymomot/forge/ops/supervisor"
)

const addr = ":8080"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	sh := newShop()

	// Messaging fabric: one broker underneath everything. The outbox wrapper
	// adds transactional push (queue.TxPusher) to a broker that lacks it — in
	// production the store is a transactional outbox.Store over the business
	// database and the broker is durable.
	inner := queue.NewMemoryBroker()
	outboxStore := outbox.NewMemoryStore()
	broker := outbox.Wrap(outboxStore, inner)
	bus := eventbus.New(broker)
	client := queue.NewClient(broker)

	// Domain wiring: the fulfillment workflow and the event subscriptions
	// that react to the order lifecycle.
	eng := workflow.NewEngine(inner, workflow.NewMemoryStore(), workflow.WithLogger(log))
	fulfill := newFulfillment(sh, bus, log)
	workflow.Register(eng, fulfill)
	subscribeOrderLifecycle(bus, eng, fulfill, log)
	if err := routeAnalytics(bus); err != nil {
		fatal(log, "route analytics", err)
	}

	// HTTP layer: order intake, analytics receiver, request telemetry.
	telemetry, err := newTelemetry(log)
	if err != nil {
		fatal(log, "build telemetry", err)
	}
	server := newShopAPI(sh, bus, telemetry, log)

	// Background services: each concern drains its own queues on the shared
	// broker. Fast polling so the demo reacts in milliseconds; production
	// keeps the defaults.
	qcfg := queue.DefaultConfig()
	qcfg.PollInterval = 100 * time.Millisecond

	worker, err := queue.NewService(broker, queue.WithConfig(qcfg), queue.WithName("shop-worker"), queue.WithLogger(log))
	if err != nil {
		fatal(log, "build queue worker", err)
	}
	busWorker, err := eventbus.NewService(bus, queue.WithConfig(qcfg), queue.WithName("eventbus-worker"), queue.WithLogger(log))
	if err != nil {
		fatal(log, "build eventbus worker", err)
	}
	wfWorker, err := workflow.NewService(eng, queue.WithConfig(qcfg), queue.WithName("workflow-worker"), queue.WithLogger(log))
	if err != nil {
		fatal(log, "build workflow worker", err)
	}

	relayCfg := outbox.DefaultConfig()
	relayCfg.PollInterval = 100 * time.Millisecond
	relay, err := outbox.NewRelay(outboxStore, inner, outbox.WithConfig(relayCfg), outbox.WithLogger(log))
	if err != nil {
		fatal(log, "build outbox relay", err)
	}

	sched, err := scheduler.New(client, scheduler.WithLogger(log))
	if err != nil {
		fatal(log, "build scheduler", err)
	}
	registerReporting(sched, worker, sh, client, telemetry, log)

	// Run everything under the supervisor; Ctrl+C drains all of it.
	ctx, stop := supervisor.NewContext()
	defer stop()

	go seed(ctx, log)

	err = supervisor.Run(ctx,
		supervisor.WithLogger(log),
		supervisor.WithService(server),
		supervisor.WithService(worker),
		supervisor.WithService(busWorker),
		supervisor.WithService(wfWorker),
		supervisor.WithService(relay),
		supervisor.WithService(sched),
		supervisor.WithService(telemetry),
	)
	if err != nil {
		fatal(log, "supervisor stopped", err)
	}
}

func fatal(log *slog.Logger, msg string, err error) {
	log.Error(msg, slog.Any("err", err))
	os.Exit(1)
}
