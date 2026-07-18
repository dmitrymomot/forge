// Command redirector is a fully working short-link service backed by
// Postgres: geo-aware smart routing (web/smartlink + smartlink/pgstore),
// click tracking with fingerprint-based duplicate detection
// (web/fingerprint), and a minimal dashboard — all on one port, split by
// host via web/hostrouter using lvh.me (which resolves to 127.0.0.1):
//
//   - http://go.lvh.me:8080/{code} — the public redirector
//   - http://app.lvh.me:8080/ — dashboard: per-link stats, create offer, create ref link
//
// Links, offers, and clicks all live in Postgres (docker-compose ships a
// postgres:18 service; migrations apply automatically on startup via
// data/migration + data/postgres). There is no seeded data — create an offer
// and a link in the dashboard, then click around:
//
//	cd examples/redirector
//	just run                                    # docker compose up + go run .
//	open http://app.lvh.me:8080/
//	curl -i "http://go.lvh.me:8080/<code>?geo=de"
//
// The visitor country comes from a geoip source chain: a ?geo=XX query
// override and an X-Geo-Country header (both for local testing), the
// CF-IPCountry header for deployments behind Cloudflare, and an optional
// real IP-based lookup — set GEOIP_MMDB to a MaxMind City database path
// (download GeoLite2-City.mmdb from maxmind.com) to enable it. Direct
// localhost clicks always fall through to the overrides: a loopback IP
// exists in no geo database. Each click's
// fingerprint (headers + client hints + IP) becomes the Visit sticky key, so
// split bucketing and duplicate detection agree by construction.
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/ops/logger"
	"github.com/dmitrymomot/forge/ops/supervisor"
	"github.com/dmitrymomot/forge/web/clientip"
	"github.com/dmitrymomot/forge/web/cookie"
	"github.com/dmitrymomot/forge/web/csrf"
	"github.com/dmitrymomot/forge/web/fingerprint"
	"github.com/dmitrymomot/forge/web/geoip"
	"github.com/dmitrymomot/forge/web/geoip/mmdb"
	"github.com/dmitrymomot/forge/web/hostrouter"
	"github.com/dmitrymomot/forge/web/httpserver"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/recoverer"
	"github.com/dmitrymomot/forge/web/requestid"
	"github.com/dmitrymomot/forge/web/requestlog"
	"github.com/dmitrymomot/forge/web/smartlink"
	"github.com/dmitrymomot/forge/web/smartlink/pgstore"
	"github.com/dmitrymomot/forge/web/useragent"
)

const (
	addr          = ":8080"
	redirectHost  = "go.lvh.me"
	dashboardHost = "app.lvh.me"
	baseURL       = "http://go.lvh.me:8080"
	dashboardURL  = "http://app.lvh.me:8080"

	// defaultDatabaseURL matches the docker-compose postgres service.
	defaultDatabaseURL = "postgres://redirector:redirector@localhost:5433/redirector?sslmode=disable"
)

//go:embed migrations/*.sql
var migrationsRaw embed.FS

func main() {
	log, err := logger.New(logger.WithContextExtractors(requestid.LogExtractor, clientip.LogExtractor))
	if err != nil {
		panic(err)
	}
	if err := run(log); err != nil {
		log.Error("redirector exited", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := supervisor.NewContext()
	defer stop()

	pool, err := openDB(ctx, log)
	if err != nil {
		return err
	}
	defer postgres.Close(pool, log)

	// Demo-only fixed secret so fingerprints stay stable across restarts;
	// a real deployment loads it from configuration.
	fp, err := fingerprint.New(fingerprint.Config{
		Secret:   "redirector-demo-secret",
		Version:  1,
		TokenTTL: 10 * time.Minute,
	}, fingerprint.WithCollectors(fingerprint.Headers(), fingerprint.ClientHints(), fingerprint.ClientIP()))
	if err != nil {
		return err
	}

	offers := NewOfferStore(pool)
	specCache, err := smartlink.NewCache(offers.Load)
	if err != nil {
		return err
	}
	tracker := NewTracker(pool, log)

	manager, err := smartlink.NewManager(pgstore.New(pool),
		smartlink.WithBaseURL(baseURL),
		smartlink.WithResolver(specCache.Resolver()),
		smartlink.WithVisitFunc(buildVisit(fp)),
		smartlink.WithOnHit(tracker.OnHit),
		smartlink.WithLogger(log),
	)
	if err != nil {
		return err
	}

	geoSrc, closeGeo, err := geoSource(log)
	if err != nil {
		return err
	}
	defer closeGeo()

	redirectMux := http.NewServeMux()
	redirectMux.Handle("/{$}", http.RedirectHandler(dashboardURL, http.StatusFound))
	redirectMux.Handle("/{code}", manager.Handler())

	dash := &Dashboard{manager: manager, offers: offers, cache: specCache, tracker: tracker, log: log}

	// Double-submit CSRF for the dashboard's POST forms. The demo runs plain
	// HTTP on lvh.me, so the cookie codec drops Secure and the token cookie
	// cannot use the default __Host- name (browsers reject __Host- cookies
	// over HTTP); a real deployment keeps both defaults. Demo-only fixed key,
	// like the fingerprint secret.
	ks, err := keyset.New(keyset.WithPrimary(1, []byte("redirector-demo-cookie-secret-key-01")))
	if err != nil {
		return err
	}
	codec, err := cookie.New(ks, cookie.WithSecure(false))
	if err != nil {
		return err
	}
	csrfMW := csrf.New(codec, csrf.WithCookieName("csrf"))

	router := hostrouter.New(
		hostrouter.WithHost(redirectHost, middleware.Wrap(redirectMux, geoip.Middleware(geoSrc))),
		hostrouter.WithHost(dashboardHost, middleware.Wrap(dash.Handler(), csrfMW)),
		hostrouter.WithHost("lvh.me", http.RedirectHandler(dashboardURL, http.StatusFound)),
	)

	h := middleware.Wrap(router,
		recoverer.New(recoverer.WithLogger(log)),
		requestid.New(),
		clientip.Middleware(clientip.TrustPrivateProxies()),
		requestlog.New(log),
	)

	srv := httpserver.New(h,
		httpserver.WithAddr(addr),
		httpserver.WithName("redirector"),
		httpserver.WithLogger(log),
	)

	log.Info("redirector up", "dashboard", dashboardURL, "redirects", baseURL+"/{code}")

	return supervisor.Run(ctx,
		supervisor.WithLogger(log),
		supervisor.WithService(srv),
		supervisor.WithService(tracker),
	)
}

// openDB connects to Postgres (DATABASE_URL, defaulting to the
// docker-compose service) and applies both migration sets — the pgstore
// links table and this example's offers/clicks tables — each under its own
// goose version table.
func openDB(ctx context.Context, log *slog.Logger) (pool *pgxpool.Pool, err error) {
	cfg := postgres.DefaultConfig()
	cfg.URL = os.Getenv("DATABASE_URL")
	if cfg.URL == "" {
		cfg.URL = defaultDatabaseURL
	}

	appMigrations, err := fs.Sub(migrationsRaw, "migrations")
	if err != nil {
		return nil, err
	}

	return postgres.Open(ctx,
		postgres.WithConfig(cfg),
		postgres.WithLogger(log),
		postgres.WithMigrator(migration.Group(
			migration.Source(pgstore.Migrations, "forge_smartlink_schema"),
			migration.Source(appMigrations, "redirector_schema"),
		)),
	)
}

// geoSource builds the visitor-country resolution chain: the ?geo=XX query
// override and X-Geo-Country header (local testing), the CF-IPCountry header
// (deployments behind Cloudflare), and — when GEOIP_MMDB points at a MaxMind
// City database (GeoLite2-City.mmdb or the GeoIP2 test fixture) — a real
// IP-based lookup via geoip/mmdb. The mmdb source resolves the client IP
// through the installed clientip middleware, so a trusted X-Forwarded-For
// chain geolocates correctly. Note that private/loopback IPs (every direct
// localhost click) exist in no geo database — that is what the query/header
// overrides are for.
func geoSource(log *slog.Logger) (geoip.Source, func(), error) {
	sources := []geoip.Source{
		queryGeo{},
		geoip.Headers(geoip.HeaderMap{Country: "X-Geo-Country"}),
		geoip.Cloudflare(),
	}
	cleanup := func() {}
	if path := os.Getenv("GEOIP_MMDB"); path != "" {
		reader, err := mmdb.New(mmdb.WithCity(path))
		if err != nil {
			return nil, nil, fmt.Errorf("open GEOIP_MMDB %q: %w", path, err)
		}
		sources = append(sources, geoip.FromLocator(reader))
		cleanup = func() {
			if err := reader.Close(); err != nil {
				log.Warn("close mmdb reader", "error", err)
			}
		}
		log.Info("geoip: mmdb city database loaded", "path", path)
	}
	return geoip.Chain(sources...), cleanup, nil
}

// queryGeo lets local tests pick a country per request:
// curl "http://go.lvh.me:8080/<code>?geo=de".
type queryGeo struct{}

func (queryGeo) Lookup(r *http.Request) (geoip.Location, error) {
	if cc := r.URL.Query().Get("geo"); len(cc) == 2 {
		return geoip.Location{CountryCode: strings.ToUpper(cc)}, nil
	}
	return geoip.Location{}, nil
}

// buildVisit assembles the smartlink Visit from request facts: country from
// the geoip middleware, device class from web/useragent, locale from
// Accept-Language, and the fingerprint hash as the sticky key.
func buildVisit(fp *fingerprint.Fingerprinter) smartlink.VisitFunc {
	return func(r *http.Request, v smartlink.Visit) smartlink.Visit {
		delete(v.Params, "geo") // local-testing override, not a real sub-ID
		v.Country = geoip.Get(r).CountryCode
		v.Device = string(useragent.ParseRequest(r).Device.Type)
		v.Locale = firstLocale(r.Header.Get("Accept-Language"))
		if f, err := fp.FromRequest(r); err == nil {
			v.StickyKey = f.Hash
		}
		return v
	}
}

// firstLocale extracts the first tag from an Accept-Language header
// ("de-DE,de;q=0.9" -> "de-DE").
func firstLocale(al string) string {
	tag, _, _ := strings.Cut(al, ",")
	tag, _, _ = strings.Cut(tag, ";")
	return strings.TrimSpace(tag)
}
