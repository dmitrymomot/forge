# Multi-Domain Routing Example

Demonstrates host-based routing with exact match, wildcard, and fallback.

## Run

```bash
go run .
```

## Test

```bash
curl http://lvh.me:8081/               # Landing: Home
curl http://api.lvh.me:8081/health     # API: OK
curl http://acme.lvh.me:8081/          # Tenant [acme]: Dashboard
```

`lvh.me` resolves to `127.0.0.1`.
