# Examples

Runnable examples for the forge packages. Each is a standalone `main` program;
run it with `go run` from the repository root.

## worker

A single background worker supervised by [`supervisor`](../supervisor). It
implements `supervisor.Service` and logs a heartbeat every 5 seconds, stopping
gracefully on `Ctrl+C` (SIGINT) or SIGTERM.

```sh
go run ./examples/worker
```

## helloworld

A plain-HTTP (no TLS) "hello world" server built with
[`httpserver`](../httpserver) and run under the supervisor for graceful
shutdown.

```sh
go run ./examples/helloworld
# in another terminal:
curl http://localhost:8080/
# -> Hello, World!
```

## orderflow

An integration demo of the whole `async/` family — [`queue`](../async/queue), [`eventbus`](../async/eventbus), [`eventrouter`](../async/eventrouter), [`outbox`](../async/outbox), [`scheduler`](../async/scheduler), [`collector`](../async/collector), and [`workflow`](../async/workflow) — wired together as one order-processing backend under the supervisor. Everything is in-memory in a single binary; the wiring stays identical when durable Store/Broker implementations replace the memory constructors.

The pipeline: `POST /orders` publishes `order.placed` transactionally through the outbox (`PublishTx` intent row → relay → broker); an inbox-guarded eventbus subscription starts the `order.fulfill` workflow (charge → reserve stock → complete, with refund compensation on out-of-stock); completion publishes `order.completed`, which a receipt subscription consumes and the eventrouter batches to the demo's own `/analytics` endpoint; the collector buffers per-request telemetry write-behind; the scheduler enqueues a `shop.report` queue job every 15 s that logs order counts and stats. See [orderflow/README.md](orderflow/README.md) for the full walkthrough with ASCII diagrams of both order paths.

```sh
go run ./examples/orderflow
# it seeds one fulfilling and one compensating order; add more:
curl -s localhost:8080/orders -d '{"item":"espresso","qty":1}'   # charged → reserved → receipt + analytics
curl -s localhost:8080/orders -d '{"item":"unicorn","qty":1}'    # out of stock → workflow.Fail → refund
```
