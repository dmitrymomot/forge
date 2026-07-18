package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/web/smartlink"
)

// GeoRule sends visitors from the listed countries to URL.
type GeoRule struct {
	URL       string   `json:"url"`
	Countries []string `json:"countries"`
}

// Offer is the consumer-side routing record a Ref-backed Link points at:
// ordered geo rules with a mandatory default destination. It lives in the
// app's own redirector_offers table; smartlink only ever sees it through the
// Cache load func as a hydrated [smartlink.Spec].
type Offer struct {
	CreatedAt  time.Time
	Ref        string
	Name       string
	DefaultURL string
	Rules      []GeoRule
}

// Spec hydrates the offer into the smartlink rule vocabulary. Rule names
// carry the row index so two rows over the same countries stay unique.
func (o Offer) Spec() smartlink.Spec {
	rules := make([]smartlink.Rule, 0, len(o.Rules))
	for i, r := range o.Rules {
		rules = append(rules, smartlink.Rule{
			Name:    fmt.Sprintf("geo-%d-%s", i+1, strings.ToLower(strings.Join(r.Countries, "-"))),
			When:    []smartlink.Matcher{smartlink.Geo{Countries: r.Countries}},
			Targets: []smartlink.Target{{URL: r.URL}},
		})
	}
	// ParamsFill forwards visit params (sub-IDs, the link's stamped campaign
	// metadata) onto the destination URL without clobbering its own query.
	return smartlink.Spec{
		Rules:   rules,
		Default: []smartlink.Target{{URL: o.DefaultURL}},
		Params:  smartlink.ParamsFill,
	}
}

// errOfferNotFound reports an unknown offer ref.
var errOfferNotFound = errors.New("offer not found")

// OfferStore persists offers in the redirector_offers table.
type OfferStore struct {
	pool *pgxpool.Pool
}

func NewOfferStore(pool *pgxpool.Pool) *OfferStore {
	return &OfferStore{pool: pool}
}

func (s *OfferStore) Save(ctx context.Context, o Offer) error {
	rules, err := json.Marshal(o.Rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO redirector_offers (ref, name, default_url, rules, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (ref) DO UPDATE
		SET name = EXCLUDED.name, default_url = EXCLUDED.default_url, rules = EXCLUDED.rules`,
		o.Ref, o.Name, o.DefaultURL, rules, o.CreatedAt.UTC())
	return err
}

func (s *OfferStore) Get(ctx context.Context, ref string) (Offer, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT ref, name, default_url, rules, created_at
		FROM redirector_offers WHERE ref = $1`, ref)
	o, err := scanOffer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Offer{}, fmt.Errorf("%w: %q", errOfferNotFound, ref)
	}
	return o, err
}

// List returns offers newest first.
func (s *OfferStore) List(ctx context.Context) ([]Offer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ref, name, default_url, rules, created_at
		FROM redirector_offers ORDER BY created_at DESC, ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Offer
	for rows.Next() {
		o, err := scanOffer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func scanOffer(row pgx.Row) (Offer, error) {
	var (
		o     Offer
		rules []byte
	)
	if err := row.Scan(&o.Ref, &o.Name, &o.DefaultURL, &rules, &o.CreatedAt); err != nil {
		return Offer{}, err
	}
	if err := json.Unmarshal(rules, &o.Rules); err != nil {
		return Offer{}, fmt.Errorf("unmarshal rules: %w", err)
	}
	return o, nil
}

// Load is the [smartlink.NewCache] load func. An unknown ref wraps
// [smartlink.ErrRefNotFound] so Manager.Create's precheck reads it as caller
// input error, not infrastructure failure.
func (s *OfferStore) Load(ctx context.Context, ref string) (smartlink.Spec, error) {
	o, err := s.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, errOfferNotFound) {
			return smartlink.Spec{}, fmt.Errorf("offer %q: %w", ref, smartlink.ErrRefNotFound)
		}
		return smartlink.Spec{}, err
	}
	return o.Spec(), nil
}
