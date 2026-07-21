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

## redirector

A fully working, Postgres-backed short-link service built from [`smartlink`](../web/smartlink) + [`smartlink/pgstore`](../web/smartlink/pgstore), [`fingerprint`](../web/fingerprint), [`geoip`](../web/geoip), [`useragent`](../web/useragent), [`hostrouter`](../web/hostrouter), [`data/postgres`](../data/postgres) + [`data/migration`](../data/migration), and [`render`](../web/render) (std `html/template`): geo-aware smart routing, click tracking with fingerprint-based duplicate detection, and a minimal dashboard with per-link stats plus create-offer and create-ref-link pages. One port, two hosts via `lvh.me` (resolves to 127.0.0.1): `go.lvh.me:8080` serves redirects, `app.lvh.me:8080` the dashboard.

Links, offers, and clicks all live in Postgres (docker-compose ships a `postgres:18` service; both migration sets apply automatically on startup). Nothing is seeded — create an offer and a link through the dashboard forms, then click around:

```sh
cd examples/redirector
just run          # docker compose up --wait + go run .
# open http://app.lvh.me:8080/ — create an offer, then a link, then:
curl -i "http://go.lvh.me:8080/<code>?geo=de"   # geo rule
curl -i "http://go.lvh.me:8080/<code>?sub=abc"  # default target, sub-ID forwarded
just stop         # stop Postgres (or `just reset` to wipe data)
```

Visitor country resolution is a chain: the `?geo=XX` query override and `X-Geo-Country` header (local testing), the `CF-IPCountry` header (behind Cloudflare), and an optional real IP lookup — set `GEOIP_MMDB` to a MaxMind City database to enable it (download `GeoLite2-City.mmdb` from maxmind.com; a direct localhost click still needs the overrides, since loopback IPs exist in no geo database). Smoke-test the IP path with the repo's MaxMind fixture:

```sh
GEOIP_MMDB=../../web/geoip/mmdb/testdata/GeoIP2-City-Test.mmdb just run
curl -i -H "X-Forwarded-For: 216.160.83.56" "http://go.lvh.me:8080/<code>"  # resolves as US
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

An integration demo of the whole `async/` family — [`queue`](../async/queue), [`eventbus`](../async/eventbus), [`eventrouter`](../async/eventrouter), [`outbox`](../async/outbox), [`scheduler`](../async/scheduler), [`collector`](../async/collector), and [`workflow`](../async/workflow) — wired together as one order-processing backend under the supervisor. Everything is in-memory in a single binary; the wiring is identical with the postgres/redis siblings swapped in.

The pipeline: `POST /orders` publishes `order.placed` transactionally through the outbox (`PublishTx` intent row → relay → broker); an inbox-guarded eventbus subscription starts the `order.fulfill` workflow (charge → reserve stock → complete, with refund compensation on out-of-stock); completion publishes `order.completed`, which a receipt subscription consumes and the eventrouter batches to the demo's own `/analytics` endpoint; the collector buffers per-request telemetry write-behind; the scheduler enqueues a `shop.report` queue job every 15 s that logs order counts and stats. See [orderflow/README.md](orderflow/README.md) for the full walkthrough with ASCII diagrams of both order paths.

```sh
go run ./examples/orderflow
# it seeds one fulfilling and one compensating order; add more:
curl -s localhost:8080/orders -d '{"item":"espresso","qty":1}'   # charged → reserved → receipt + analytics
curl -s localhost:8080/orders -d '{"item":"unicorn","qty":1}'    # out of stock → workflow.Fail → refund
```
