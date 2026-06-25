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
