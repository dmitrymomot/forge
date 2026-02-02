# Multi-Domain Routing Example

This example demonstrates Forge's `hostrouter` package for host-based routing with subdomains.

## What it Shows

- **Exact host matching**: `api.lvh.me` routes to a JSON API
- **Wildcard host matching**: `*.lvh.me` routes to tenant dashboards
- **Fallback routing**: Unmatched hosts use the default handlers
- **Mixed handler styles**: Both `forge.Handler` and raw `http.Handler` work with host routing
- **Scoped middleware**: Tenant extraction only on tenant router, JSON content-type only on API router

## Why lvh.me?

[lvh.me](http://lvh.me) is a domain that resolves to `127.0.0.1`. This allows testing subdomain routing locally without editing `/etc/hosts`.

## Routing Structure

| URL                    | Host Pattern        | Handler                           |
| ---------------------- | ------------------- | --------------------------------- |
| http://app.lvh.me:8081 | Fallback            | LandingHandler (main site)        |
| http://api.lvh.me:8081 | `api.lvh.me` exact  | APIHandler (JSON API)             |
| http://\*.lvh.me:8081  | `*.lvh.me` wildcard | TenantHandler (tenant dashboards) |

## Running

```bash
# Generate templ files and run
task run

# Or manually
templ generate
go run .
```

## Test URLs

Once running, try these URLs in your browser:

### Main Site (Fallback Handler)

- http://app.lvh.me:8081 - Landing page
- http://app.lvh.me:8081/features - Features page
- http://app.lvh.me:8081/pricing - Pricing page

### API Domain (Exact Match)

- http://api.lvh.me:8081/health - Health check (JSON)
- http://api.lvh.me:8081/tenants - List all tenants (JSON)
- http://api.lvh.me:8081/tenants/acme - Get specific tenant (JSON)

### Tenant Subdomains (Wildcard Match)

- http://acme.lvh.me:8081 - Acme Corp dashboard
- http://demo.lvh.me:8081 - Demo Inc dashboard
- http://test.lvh.me:8081 - Test Co dashboard
- http://anything.lvh.me:8081 - Works for any subdomain!

## Code Structure

```
examples/multi-domain/
├── main.go                    # Bootstrap with WithHostRoutes
├── middleware/
│   └── tenant.go              # Subdomain → context extraction
├── handlers/
│   ├── landing.go             # Main site (forge.Handler)
│   ├── api.go                 # JSON API (http.Handler on chi)
│   └── tenant.go              # Tenant pages (http.Handler on chi)
├── views/
│   ├── layout.templ           # Shared HTML layout
│   ├── landing.templ          # Landing page views
│   └── tenant.templ           # Tenant dashboard views
├── Taskfile.yml               # Build tasks
└── README.md                  # This file
```

## Key Implementation Details

### Host Routing Setup (main.go)

```go
app := forge.New(
    forge.WithHostRoutes(forge.HostRoutes{
        "api.lvh.me": apiRouter,    // Exact match
        "*.lvh.me":   tenantRouter, // Wildcard for subdomains
    }),
    forge.WithHandlers(handlers.NewLandingHandler()), // Fallback
)
```

### Subdomain Extraction (middleware/tenant.go)

The tenant middleware:

1. Extracts subdomain from `r.Host` (e.g., "acme.lvh.me:8081" → "acme")
2. Rejects reserved subdomains (api, app, www) with 400
3. Stores tenant in context via custom key type
4. Provides `TenantFromContext(ctx)` helper

### Mixed Handler Styles

- **LandingHandler**: Implements `forge.Handler` interface for the fallback
- **API handlers**: Raw `http.HandlerFunc` on chi router
- **Tenant handlers**: Raw `http.HandlerFunc` on chi router with tenant middleware

This shows that `WithHostRoutes` accepts any `http.Handler`, giving you flexibility in how you structure different domains.
