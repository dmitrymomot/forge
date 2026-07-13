package rng

import (
	"context"
	"errors"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/random"
)

// Seed is the public view of a seed pair. ServerSeed is nil until the
// pair is revealed — active pairs expose only the Commitment.
type Seed struct {
	CreatedAt  time.Time
	RevealedAt time.Time
	ID         string
	PlayerID   string
	Commitment string
	ClientSeed string
	Status     string
	Algorithm  string
	ServerSeed []byte
	Nonce      uint64
}

// Proof is what the consumer persists on a game round — everything a
// verify page needs to recompute the outcome after reveal.
type Proof struct {
	SeedID     string
	Commitment string
	ClientSeed string
	Algorithm  string
	Nonce      uint64
}

// Manager owns the provably-fair seed-pair lifecycle over a Store:
// commit-reveal, atomic per-round nonce consumption, and rotation.
type Manager struct {
	store Store
	cfg   config
}

// NewManager builds a Manager over store.
func NewManager(store Store, opts ...Option) (*Manager, error) {
	cfg := config{clock: clock.System()}
	for _, o := range opts {
		o(&cfg)
	}
	var errs []error
	if store == nil {
		errs = append(errs, errors.New("rng: nil store"))
	}
	if cfg.scopeSet && cfg.scope == nil {
		errs = append(errs, errors.New("rng: nil scope hook"))
	}
	if cfg.clockSet && cfg.clock == nil {
		errs = append(errs, errors.New("rng: nil clock"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &Manager{store: store, cfg: cfg}, nil
}

var errEmptyPlayerID = errors.New("rng: player id must not be empty")

func (m *Manager) scopeFrom(ctx context.Context) (string, error) {
	if m.cfg.scope == nil {
		return "", nil
	}
	s, err := m.cfg.scope(ctx)
	if err != nil {
		return "", errors.Join(ErrNoScope, err)
	}
	if s == "" {
		return "", ErrNoScope
	}
	return s, nil
}

// toSeed converts a Record to its public view, exposing the server seed
// only once revealed.
func toSeed(r Record) Seed {
	s := Seed{
		ID:         r.ID,
		PlayerID:   r.PlayerID,
		Commitment: Commitment(r.ServerSeed),
		ClientSeed: r.ClientSeed,
		Nonce:      r.Nonce,
		Status:     r.Status,
		Algorithm:  r.Algorithm,
		CreatedAt:  r.CreatedAt,
		RevealedAt: r.RevealedAt,
	}
	if r.Status == StatusRevealed {
		s.ServerSeed = append([]byte(nil), r.ServerSeed...)
	}
	return s
}

// createPair inserts a fresh committed pair; empty clientSeed means
// generate one. Returns ErrExists unwrapped so callers can retry reads.
func (m *Manager) createPair(ctx context.Context, scope, playerID, clientSeed string) (Record, error) {
	if clientSeed == "" {
		clientSeed = random.String(defaultClientSeedLen, clientSeedAlphabet)
	} else if !validClientSeed(clientSeed) {
		return Record{}, ErrInvalidClientSeed
	}
	rec := Record{
		ID:         id.NewUUID().String(),
		Scope:      scope,
		PlayerID:   playerID,
		ServerSeed: random.Bytes(serverSeedLen),
		ClientSeed: clientSeed,
		Status:     StatusActive,
		Algorithm:  Algorithm,
		CreatedAt:  m.cfg.clock.Now().UTC(),
	}
	if err := m.store.Create(ctx, rec); err != nil {
		if errors.Is(err, ErrExists) {
			return Record{}, ErrExists
		}
		return Record{}, errors.Join(ErrStore, err)
	}
	return rec, nil
}

// ActiveSeed returns the player's active pair, creating one if none
// exists. The returned Seed carries the Commitment for the fairness UI;
// the server seed itself is never exposed while active.
func (m *Manager) ActiveSeed(ctx context.Context, playerID string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	if playerID == "" {
		return Seed{}, errEmptyPlayerID
	}
	rec, err := m.store.Active(ctx, scope, playerID)
	switch {
	case err == nil:
		return toSeed(rec), nil
	case errors.Is(err, ErrNotFound):
		created, cerr := m.createPair(ctx, scope, playerID, "")
		if errors.Is(cerr, ErrExists) { // lost a create race — read the winner
			rec, err = m.store.Active(ctx, scope, playerID)
			if err != nil {
				return Seed{}, errors.Join(ErrStore, err)
			}
			return toSeed(rec), nil
		}
		if cerr != nil {
			return Seed{}, cerr
		}
		return toSeed(created), nil
	default:
		return Seed{}, errors.Join(ErrStore, err)
	}
}

// Play atomically consumes the next nonce of the player's active pair and
// returns the derived Stream plus the Proof to persist on the game round.
// The pair is created on first play; a rotate that crashed between reveal
// and create is healed here.
func (m *Manager) Play(ctx context.Context, playerID string) (*Stream, Proof, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return nil, Proof{}, err
	}
	if playerID == "" {
		return nil, Proof{}, errEmptyPlayerID
	}
	for range 3 { // bounded retries against concurrent create/rotate races
		rec, err := m.store.ConsumeNonce(ctx, scope, playerID)
		if errors.Is(err, ErrNotFound) {
			if _, cerr := m.createPair(ctx, scope, playerID, ""); cerr != nil && !errors.Is(cerr, ErrExists) {
				return nil, Proof{}, cerr
			}
			continue
		}
		if err != nil {
			return nil, Proof{}, errors.Join(ErrStore, err)
		}
		s, err := New(rec.ServerSeed, rec.ClientSeed, rec.Nonce)
		if err != nil {
			return nil, Proof{}, errors.Join(ErrStore, err) // corrupted record
		}
		return s, Proof{
			SeedID:     rec.ID,
			Commitment: Commitment(rec.ServerSeed),
			ClientSeed: rec.ClientSeed,
			Nonce:      rec.Nonce,
			Algorithm:  rec.Algorithm,
		}, nil
	}
	return nil, Proof{}, errors.Join(ErrStore, errors.New("rng: no active seed after retries"))
}

// SetClientSeed rotates the player's pair onto clientSeed: the current
// pair (if any) is revealed and a fresh committed pair is created with
// the given client seed, so played (pair, nonce) history is never
// mutated. Creates the first pair when none exists. A concurrent writer
// racing the rotation returns an error matching both ErrStore and
// ErrExists; the caller should retry.
func (m *Manager) SetClientSeed(ctx context.Context, playerID, clientSeed string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	if playerID == "" {
		return Seed{}, errEmptyPlayerID
	}
	if !validClientSeed(clientSeed) {
		return Seed{}, ErrInvalidClientSeed
	}
	cur, err := m.store.Active(ctx, scope, playerID)
	switch {
	case err == nil:
		if _, rerr := m.store.Reveal(ctx, scope, cur.ID, m.cfg.clock.Now().UTC()); rerr != nil {
			return Seed{}, errors.Join(ErrStore, rerr)
		}
	case !errors.Is(err, ErrNotFound):
		return Seed{}, errors.Join(ErrStore, err)
	}
	rec, err := m.createPair(ctx, scope, playerID, clientSeed)
	if errors.Is(err, ErrExists) {
		return Seed{}, errors.Join(ErrStore, err) // concurrent writer; caller retries
	}
	if err != nil {
		return Seed{}, err
	}
	return toSeed(rec), nil
}

// Rotate reveals the player's active pair — the returned old Seed carries
// the ServerSeed for verification — and creates a fresh committed pair
// inheriting the current client seed. ErrNotFound without an active pair.
// A concurrent writer racing the rotation returns an error matching both
// ErrStore and ErrExists; the caller should retry.
func (m *Manager) Rotate(ctx context.Context, playerID string) (Seed, Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, Seed{}, err
	}
	if playerID == "" {
		return Seed{}, Seed{}, errEmptyPlayerID
	}
	cur, err := m.store.Active(ctx, scope, playerID)
	if errors.Is(err, ErrNotFound) {
		return Seed{}, Seed{}, ErrNotFound
	}
	if err != nil {
		return Seed{}, Seed{}, errors.Join(ErrStore, err)
	}
	revealed, err := m.store.Reveal(ctx, scope, cur.ID, m.cfg.clock.Now().UTC())
	if err != nil {
		return Seed{}, Seed{}, errors.Join(ErrStore, err)
	}
	next, err := m.createPair(ctx, scope, playerID, cur.ClientSeed)
	if errors.Is(err, ErrExists) {
		return Seed{}, Seed{}, errors.Join(ErrStore, err) // concurrent writer; caller retries
	}
	if err != nil {
		return Seed{}, Seed{}, err
	}
	return toSeed(revealed), toSeed(next), nil
}

// Seed returns a pair by id for verify pages. The ServerSeed is included
// only once the pair is revealed.
func (m *Manager) Seed(ctx context.Context, seedID string) (Seed, error) {
	scope, err := m.scopeFrom(ctx)
	if err != nil {
		return Seed{}, err
	}
	rec, err := m.store.Get(ctx, scope, seedID)
	if errors.Is(err, ErrNotFound) {
		return Seed{}, ErrNotFound
	}
	if err != nil {
		return Seed{}, errors.Join(ErrStore, err)
	}
	return toSeed(rec), nil
}
