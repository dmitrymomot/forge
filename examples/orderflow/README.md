# orderflow

An integration demo that wires **every `async/` package** — [`queue`](../../async/queue), [`eventbus`](../../async/eventbus), [`eventrouter`](../../async/eventrouter), [`outbox`](../../async/outbox), [`workflow`](../../async/workflow), [`scheduler`](../../async/scheduler), and [`collector`](../../async/collector) — into one coherent order-processing backend, to prove they compose. Each package plays its natural role and each stage's output is the next stage's input.

Everything is in-memory in a single binary: no Docker, no external services. The memory constructors (`queue.NewMemoryBroker`, `outbox.NewMemoryStore`, `workflow.NewMemoryStore`, `eventbus.NewMemoryInbox`, the default scheduler store) are drop-in stand-ins for their postgres/redis siblings — swap the constructors and the wiring stays identical.

## The scenario

A tiny web shop. `POST /orders` accepts an order, the backend charges payment, reserves stock, and emails a receipt — asynchronously, durably, with a compensating refund when stock runs out. Analytics events stream to a "warehouse", request telemetry is collected write-behind, and a scheduled job logs a shop summary every 15 seconds.

Two items exist: `espresso` (stock 100, always fulfills) and `unicorn` (stock 0, always fails and demonstrates compensation).

## The big picture

```
                        ┌─────────────────────────────────────────────────────────────┐
                        │            one binary, 7 services under ops/supervisor      │
                        └─────────────────────────────────────────────────────────────┘

  curl POST /orders
        │
        ▼
  ┌───────────┐  Add(request)   ┌───────────┐  batch every 5s
  │ shop-api  │────────────────▶│ collector │─────────────────▶ "telemetry flushed" log
  │(httpserver│                 └───────────┘                    (write-behind, lossy by contract)
  │ +telemetry│
  │middleware)│                                 ┌──────────────┐
  └───────────┘                            ┌───▶│ outbox relay │───┐
        │ eventbus.PublishTx(order.placed) │    └──────────────┘   │ Push
        ▼                                  │ poll                  ▼
  ┌──────────────┐                         │              ┌────────────────┐
  │ outbox store │─────────────────────────┘              │  queue broker  │
  │ (intent rows)│   row committed with the business tx   │    (memory)    │
  └──────────────┘                                        └────────────────┘
                                                                   │
                       one durable job per (event, subscription)   │  eventbus fan-out
              ┌───────────────────────────────┬────────────────────┤
              ▼                               ▼                    ▼
     ┌─────────────────┐             ┌─────────────────┐   ┌───────────────────┐
     │ "fulfill" sub   │             │ "receipt" sub   │   │ eventrouter routes│
     │ (inbox-guarded) │             │ (order.completed│   │ order.placed +    │
     └─────────────────┘             │  → log email)   │   │ order.completed   │
              │ workflow.Start       └─────────────────┘   └───────────────────┘
              ▼                               ▲                    │ batch ≤8 events / 2s
     ┌─────────────────────────┐              │                    ▼
     │  workflow order.fulfill │              │            POST /analytics
     │  charge → reserve →     │──────────────┘            (served by this same
     │  complete               │  publish order.completed   binary = the "warehouse")
     └─────────────────────────┘

  ┌───────────┐ every 15s ┌──────────────────┐        ┌─────────────┐
  │ scheduler │──────────▶│ shop.report job  │───────▶│ shop-worker │──▶ "shop report" log
  └───────────┘  enqueue  │ (queue, default) │ claim  └─────────────┘
                          └──────────────────┘
```

## Life of a fulfilled order

```
 curl -d '{"item":"espresso","qty":2}' localhost:8080/orders
   │
   ▼
 shop-api            shop.place() → ord-1, status "placed"
   │                 eventbus.PublishTx(order.placed)      ── outbox intent row, NOT a broker
   │                                                          push: with pgoutbox it commits or
   ▼                                                          rolls back with the order insert
 outbox relay        claims the committed row, pushes it into the broker, deletes it
   │
   ▼
 eventbus            fans order.placed out: one durable job per subscription
   ├─▶ "fulfill"     inbox.Seen(id)? no → claims it → workflow.Start(order.fulfill)
   └─▶ "analytics"   (see egress below)
   │
   ▼
 workflow-worker     drives the run, checkpointing after every step:
   1. charge_payment   PaymentID = pay-ord-1              [checkpoint]
   2. reserve_stock    espresso 100 → 98, keyed by ord-1  [checkpoint]
   3. complete         status "fulfilled",
                       publish order.completed            [checkpoint, run completed]
   │
   ▼
 eventbus            fans order.completed out:
   ├─▶ "receipt"     → "receipt emailed  order=ord-1"
   └─▶ "analytics"   → joins the batch
   │
   ▼
 eventrouter         batches accumulate until 8 events or 2s of age, then one
   │                 HTTP POST delivers them; the jobs block until the flush
   ▼                 resolves, so nothing is acked before the warehouse accepted it
 POST /analytics     → "analytics ingested  event=order.placed  id=…"
                       "analytics ingested  event=order.completed  id=…"
```

## Life of an out-of-stock order (compensation)

```
 curl -d '{"item":"unicorn","qty":1}' localhost:8080/orders     unicorn stock = 0
   │
   │   ... same intake path: outbox → relay → broker → "fulfill" ...
   ▼
 workflow-worker     order.fulfill for ord-2:
   1. charge_payment   PaymentID = pay-ord-2              [checkpoint]
   2. reserve_stock    out of stock → workflow.Fail(err)  ── business failure:
   │                                                         no retry can help
   ▼
 the run flips to "compensating": COMPLETED steps undo in reverse order
   (reserve_stock never completed, so only charge_payment compensates)
   1'. refund_payment   status "failed", "payment refunded  order=ord-2"
   │
   ▼
 run ends "failed", the original error recorded on the run row:
   workflow run failed after compensation … item "unicorn" is out of stock
```

## Who does what

| Package       | Role here                                                                                        | Production swap                                     |
| ------------- | ------------------------------------------------------------------------------------------------ | --------------------------------------------------- |
| `queue`       | The engine under everything: durable jobs, leases, retry, dead-letter. Also runs the report job.  | `queue/postgres` or `queue/redis` broker            |
| `outbox`      | Makes `PublishTx` transactional on a broker that lacks it: intent row now, relay pushes it later. | `outbox/postgres` over the business DB              |
| `eventbus`    | Fans each order event out to independent subscriptions; the inbox dedups at-least-once delivery.  | same code; `eventbus/postgres` inbox                |
| `workflow`    | The fulfillment saga: checkpointed steps, compensation on permanent failure.                      | `workflow/postgres` store                           |
| `eventrouter` | Egress: batches both events to the analytics endpoint over real HTTP with queue-grade retry.      | point the deliverer at the real warehouse           |
| `scheduler`   | Turns "every 15s" into a `shop.report` queue job; claim store fires once per fleet.               | `scheduler/postgres` store                          |
| `collector`   | Write-behind request telemetry: `Add` never blocks the request path, batches flush to a sink.     | a real sink (warehouse insert), `resilience/retry`  |

All seven services run under [`ops/supervisor`](../../ops/supervisor):

```
supervisor
├── shop-api          httpserver     :8080 — /orders intake, /analytics receiver
├── shop-worker       queue.Service  drains "default"           (shop.report)
├── eventbus-worker   queue.Service  drains subscription queues (order.placed.fulfill, …receipt, …analytics)
├── workflow-worker   queue.Service  drains "order.fulfill"     (the saga's own queue)
├── outbox            relay          polls intent rows → broker
├── scheduler         tick claimer   enqueues shop.report every 15s
└── collector         flusher        drains the telemetry buffer (and on shutdown)
```

Ctrl+C cancels the shared context and the supervisor drains everything: in-flight jobs finish, the collector flushes its buffer, the server stops accepting.

## Run it

```sh
go run ./examples/orderflow
```

On startup it seeds one order of each kind, so the full pipeline shows without any curl. Then poke it:

```sh
curl -s localhost:8080/orders -d '{"item":"espresso","qty":1}'   # happy path
curl -s localhost:8080/orders -d '{"item":"unicorn","qty":1}'    # compensation path
```

What to expect in the log, in order:

```
payment charged      order=ord-1 payment=pay-ord-1
payment charged      order=ord-2 payment=pay-ord-2
stock reserved       order=ord-1 item=espresso
payment refunded     order=ord-2 payment=pay-ord-2
workflow run completed  workflow=order.fulfill …
workflow run failed after compensation … item "unicorn" is out of stock
receipt emailed      order=ord-1
analytics ingested   event=order.placed …          (≈2s later, one batch)
analytics ingested   event=order.placed …
analytics ingested   event=order.completed …
telemetry flushed    requests=3                    (≈5s, collector interval)
shop report          orders_fulfilled=1 orders_failed=1 …   (next 15s boundary)
```

## Code layout

| File                               | Contents                                                                     |
| ---------------------------------- | ---------------------------------------------------------------------------- |
| [`main.go`](main.go)               | Wiring only: broker → domain → HTTP → workers → supervisor                   |
| [`shop.go`](shop.go)               | The "business database": stock, order statuses, the two lifecycle events     |
| [`fulfillment.go`](fulfillment.go) | The `order.fulfill` workflow and the eventbus subscriptions that drive it    |
| [`web.go`](web.go)                 | HTTP handlers, telemetry middleware, analytics egress route, the demo seeder |
| [`report.go`](report.go)           | The scheduled `shop.report` job: schedule + handler                          |
