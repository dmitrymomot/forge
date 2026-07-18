package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// Click is one recorded redirect.
type Click struct {
	At          time.Time
	Code        string
	Rule        string // matched rule name; "" means the default target
	URL         string
	Country     string
	Device      string
	Fingerprint string
	Duplicate   bool
}

// ShortFP is the fingerprint hash truncated for display.
func (c Click) ShortFP() string {
	if len(c.Fingerprint) <= 12 {
		return c.Fingerprint
	}
	return c.Fingerprint[:12]
}

// RuleName is the matched rule, with the default target named explicitly.
func (c Click) RuleName() string {
	if c.Rule == "" {
		return "default"
	}
	return c.Rule
}

// ClickCounts is the per-link click summary shown on the dashboard index.
type ClickCounts struct {
	Total      int
	Unique     int
	Duplicates int
}

// LinkStats is the full aggregate for one code's detail page.
type LinkStats struct {
	ByCountry map[string]int
	ByRule    map[string]int
	Recent    []Click // newest first, capped
	ClickCounts
}

const recentCap = 20

// Tracker persists clicks in the redirector_clicks table, off the request
// path: OnHit does a non-blocking send into a buffered channel (the bounded
// sink WithOnHit demands) and the Run loop — a supervisor service and the
// single writer — inserts each click. A click is a duplicate when its
// fingerprint hash was already recorded for the code; the check is computed
// inside the INSERT, and the single-writer loop keeps check and insert
// race-free.
type Tracker struct {
	log  *slog.Logger
	pool *pgxpool.Pool
	ch   chan Click
}

func NewTracker(pool *pgxpool.Pool, log *slog.Logger) *Tracker {
	return &Tracker{log: log, pool: pool, ch: make(chan Click, 256)}
}

// OnHit is wired via [smartlink.WithOnHit]; it runs synchronously after each
// redirect is written, so it must never block.
func (t *Tracker) OnHit(_ context.Context, h smartlink.Hit) {
	c := Click{
		At:          time.Now().UTC(),
		Code:        h.Link.Code,
		Rule:        h.Decision.Rule,
		URL:         h.Decision.URL,
		Country:     strings.ToUpper(h.Visit.Country),
		Device:      h.Visit.Device,
		Fingerprint: h.Visit.StickyKey,
	}
	select {
	case t.ch <- c:
	default:
		t.log.Warn("tracker: click dropped, buffer full", "code", c.Code)
	}
}

func (t *Tracker) Name() string { return "click-tracker" }

// Run inserts queued clicks until ctx is canceled, then drains whatever is
// still buffered so a shutdown right after a click doesn't lose it.
func (t *Tracker) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			t.drain()
			return nil
		case c := <-t.ch:
			t.insert(c)
		}
	}
}

func (t *Tracker) drain() {
	for {
		select {
		case c := <-t.ch:
			t.insert(c)
		default:
			return
		}
	}
}

// insert records one click. An empty fingerprint never counts as a
// duplicate — an unidentifiable visitor is not evidence of a repeat visit.
func (t *Tracker) insert(c Click) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := t.pool.Exec(ctx, `
		INSERT INTO redirector_clicks (code, rule, url, country, device, fingerprint, is_duplicate, clicked_at)
		VALUES ($1, $2, $3, $4, $5, $6,
		        $6 <> '' AND EXISTS (SELECT 1 FROM redirector_clicks WHERE code = $1 AND fingerprint = $6),
		        $7)`,
		c.Code, c.Rule, c.URL, c.Country, c.Device, c.Fingerprint, c.At)
	if err != nil {
		t.log.Error("tracker: insert click", "code", c.Code, "error", err)
	}
}

// Summaries returns per-code click counts for every code that has clicks.
func (t *Tracker) Summaries(ctx context.Context) (map[string]ClickCounts, error) {
	rows, err := t.pool.Query(ctx, `
		SELECT code, count(*), count(*) FILTER (WHERE is_duplicate)
		FROM redirector_clicks GROUP BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]ClickCounts)
	for rows.Next() {
		var (
			code string
			c    ClickCounts
		)
		if err := rows.Scan(&code, &c.Total, &c.Duplicates); err != nil {
			return nil, err
		}
		c.Unique = c.Total - c.Duplicates
		out[code] = c
	}
	return out, rows.Err()
}

// Stats returns the full aggregate for one code; zero stats when it has no
// clicks yet.
func (t *Tracker) Stats(ctx context.Context, code string) (LinkStats, error) {
	var s LinkStats
	err := t.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE is_duplicate)
		FROM redirector_clicks WHERE code = $1`, code).Scan(&s.Total, &s.Duplicates)
	if err != nil {
		return LinkStats{}, err
	}
	s.Unique = s.Total - s.Duplicates
	if s.Total == 0 {
		return s, nil
	}

	s.ByCountry, err = t.groupCount(ctx, code, `COALESCE(NULLIF(country, ''), 'unknown')`)
	if err != nil {
		return LinkStats{}, err
	}
	s.ByRule, err = t.groupCount(ctx, code, `COALESCE(NULLIF(rule, ''), 'default')`)
	if err != nil {
		return LinkStats{}, err
	}

	rows, err := t.pool.Query(ctx, `
		SELECT clicked_at, rule, url, country, device, fingerprint, is_duplicate
		FROM redirector_clicks WHERE code = $1 ORDER BY id DESC LIMIT $2`, code, recentCap)
	if err != nil {
		return LinkStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		c := Click{Code: code}
		if err := rows.Scan(&c.At, &c.Rule, &c.URL, &c.Country, &c.Device, &c.Fingerprint, &c.Duplicate); err != nil {
			return LinkStats{}, err
		}
		c.At = c.At.Local()
		s.Recent = append(s.Recent, c)
	}
	return s, rows.Err()
}

// groupCount aggregates clicks for code by expr (a trusted SQL expression,
// never user input).
func (t *Tracker) groupCount(ctx context.Context, code, expr string) (map[string]int, error) {
	rows, err := t.pool.Query(ctx,
		`SELECT `+expr+`, count(*) FROM redirector_clicks WHERE code = $1 GROUP BY 1`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var (
			key string
			n   int
		)
		if err := rows.Scan(&key, &n); err != nil {
			return nil, err
		}
		out[key] = n
	}
	return out, rows.Err()
}
