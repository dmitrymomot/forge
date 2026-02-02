# Simple Example

A minimal single-file example demonstrating core Forge features without external dependencies.

## Features Demonstrated

- **Handler pattern** - `greetingHandler` and `echoHandler` implement `forge.Handler`
- **Routing** - GET, POST, URL parameters (`{name}`), query parameters (`?name=`)
- **Request binding** - JSON binding with validation using struct tags
- **Responses** - JSON and plain text responses
- **Middleware** - Request logging middleware
- **Health checks** - Built-in liveness/readiness endpoints
- **Error handling** - Custom error and not-found handlers
- **Graceful shutdown** - Clean shutdown on SIGINT/SIGTERM

## Running

```bash
go run examples/simple/main.go
```

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Welcome message |
| GET | `/hello/{name}` | Greet by URL parameter |
| GET | `/greet?name=X` | Greet by query parameter |
| POST | `/echo` | Echo JSON message |
| GET | `/health/live` | Liveness probe |
| GET | `/health/ready` | Readiness probe |

## Testing

```bash
# Welcome message
curl http://localhost:8080/

# URL parameter
curl http://localhost:8080/hello/World

# Query parameter
curl "http://localhost:8080/greet?name=Forge"

# JSON echo
curl -X POST http://localhost:8080/echo \
  -H "Content-Type: application/json" \
  -d '{"message":"Hello Forge"}'

# Validation error (empty message)
curl -X POST http://localhost:8080/echo \
  -H "Content-Type: application/json" \
  -d '{"message":""}'

# Health checks
curl http://localhost:8080/health/live
curl http://localhost:8080/health/ready
```
