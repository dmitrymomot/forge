# gaming/rng — Design

Deterministic random outcomes for game mechanics: weighted drop tables (lootbox), dice, wheels, card draws, multi-value derivation for slots — over one explicitly specified derivation algorithm (`rng/v1`) with two entry points: a casual CSPRNG source (zero ceremony) and a provably-fair seed-chain `Manager` (commit–reveal, Store seam, memory + `pgstore` drivers). First resident of the `gaming/` domain; the demo-games product is its committed second consumer.

Deps: `core/random`, `core/clock`, `core/id`. Driver `gaming/rng/pgstore`: `pgx`, `data/migration`. Core is otherwise stdlib (`crypto/hmac`, `crypto/sha256`).

## Architecture

Three layers, one package:

1. **`Stream`** — a deterministic, sequentially-consumed byte stream derived from `(serverSeed, clientSeed, nonce)`. All randomness flows through it. Pure, no I/O.
2. **Mechanics** — pure functions over a `Stream`: uniform ints/floats, weighted `Table` (lootbox/wheel), pity, shuffle/deal, dice. No state, no storage.
3. **`Manager`** — the stateful provably-fair lifecycle (seed pairs, commitments, atomic nonce consumption, rotation with reveal) behind a 5-method `Store` seam. `Stream` and `Table` are fully usable without it.

Rejected alternatives:

- **Riding `math/rand/v2` via a `rand.Source` adapter** — `rand.Rand` method algorithms are not a stability contract and cannot be reimplemented cross-language without porting Go internals; a player-facing JS verifier becomes impossible. Disqualifying for provable fairness.
- **Split brick + manager (`gaming/rng` + `gaming/seedchain`)** — the seed-chain manager would have exactly one consumer; fails the product-or-brick test. Isolation is achieved inside one package: `Stream` stays pure, `Manager` is the only stateful type.
- **Stateless primitives only (caller owns seed storage)** — rejected by user decision; the package owns the full lifecycle so consumers get commit–reveal correct by construction.

## Derivation spec — `rng/v1` (normative, frozen forever)

The algorithm identifier `rng/v1` is a package constant, stored on every seed record and stamped into every `Proof`. Any future algorithm change ships as `rng/v2` alongside — old outcomes must stay verifiable forever. The spec below is reproduced in `doc.go` so third parties can reimplement it in any language.

- **Server seed:** exactly 32 bytes from `crypto/rand`. **Commitment:** lowercase-hex `SHA-256(serverSeed)`, published before play.
- **Client seed:** 1–64 chars from `[A-Za-z0-9_-]`. Player-supplied; defaults to 16 random chars from that alphabet. The restricted alphabet keeps HMAC messages unambiguous (no `:` inside the seed) and verify pages copy-paste-safe.
- **Nonce:** uint64 bet counter, starts at 0, one per game round.
- **Block expansion:** `block_i = HMAC-SHA256(key = serverSeed, msg = clientSeed || ":" || decimal(nonce) || ":" || decimal(i))` for `i = 0, 1, 2, …`. The stream serves these 32-byte blocks sequentially; each draw consumes exactly the bytes it needs, crossing block boundaries transparently. One nonce yields arbitrarily many values (5 slot reels = 5 sequential draws); a verifier replays the draws in order.
- **Uint64:** next 8 bytes, big-endian.
- **IntN(n):** rejection sampling — let `limit = 2^64 − (2^64 mod n)`; draw `Uint64` until `v < limit`; return `int(v mod n)`. No modulo bias; portable (JS via BigInt).
- **Float64:** `Uint64 >> 11` divided by `2^53` — exact in IEEE 754 in every language; result in `[0, 1)`.
- **Shuffle(n):** Fisher–Yates, spec'd order: `for i := n−1; i > 0; i-- { j := IntN(i+1); swap(i, j) }`.
- **Weighted pick:** `total = Σ weights`; draw `IntN(total)`; walk cumulative weights in entry order, first bucket containing the draw wins.

Golden test vectors (hardcoded hex expectations for blocks, Uint64, IntN, Float64, Shuffle, Pick) pin the spec: a refactor that changes one output byte fails the build.

## API surface

```go
// Stream construction
func New(serverSeed []byte, clientSeed string, nonce uint64) (*Stream, error) // ErrInvalidSeed / ErrInvalidClientSeed
func Casual() *Stream // fresh CSPRNG server seed, default client seed, nonce 0 — same math, no ceremony

// Draws (sequential, deterministic; n <= 0 panics, matching math/rand)
func (s *Stream) Uint64() uint64
func (s *Stream) IntN(n int) int
func (s *Stream) Ints(count, n int) []int  // count draws of IntN(n) — slot reels
func (s *Stream) Float64() float64
func (s *Stream) Roll(sides int) int       // dice: IntN(sides) + 1
func (s *Stream) Perm(n int) []int
func (s *Stream) Shuffle(n int, swap func(i, j int))

// Draw-without-replacement sugar (cards, raffle winners); operates on a copy
func Deal[T any](s *Stream, items []T, n int) []T

// Commitment helpers
func Commitment(serverSeed []byte) string
func VerifyCommitment(serverSeed []byte, commitment string) bool
```

**Weighted tables** (lootbox and wheel are the same primitive):

```go
type Entry[T any] struct {
    Key    string // stable identity, required, unique — feeds the version hash
    Weight uint64 // relative weight, > 0
    Item   T      // payload
}

func NewTable[T any](entries []Entry[T], opts ...TableOption) (*Table[T], error) // ErrInvalidTable
func (t *Table[T]) Pick(s *Stream) Entry[T]      // returns the full entry (Key for audit logs)
func (t *Table[T]) Version() string              // hex SHA-256 over ordered (key, weight) pairs — the audit anchor
func WithPity(threshold uint64, hitKeys ...string) TableOption

// Pity, pure — caller persists misses next to the player row
func (t *Table[T]) PickWithPity(s *Stream, misses uint64) (Entry[T], uint64)
```

`NewTable` validates: non-empty, no zero weights, no duplicate/empty keys, total weight fits uint64. `WithPity` validates threshold > 0 and that every hit key exists. `PickWithPity` semantics: if `misses+1 >= threshold`, the pick is forced to the hit set (weighted among hit entries only, still drawn from the stream — deterministic and verifiable); a natural or forced hit resets the returned counter to 0, any other outcome returns `misses+1`. Calling `PickWithPity` on a table without `WithPity` panics (programmer error, documented). Pity state is never stored by the package: the counter must update atomically with granting the reward, and only the consumer's transaction can do that (the ledger's "invariants live in the caller's tx" rule).

**Cards:** `Card` value type (uint8; `Rank()`, `Suit()`, `String()` — "A♠"), `NewDeck(decks int) []Card`. Dealing = `Deal(stream, deck, n)`. No game rules (blackjack scoring etc. stays out).

## Manager — provably-fair lifecycle

```go
func NewManager(store Store, opts ...Option) (*Manager, error)

// Options
WithScope(fn func(context.Context) (string, error)) // tenancy hook; nil is a constructor error
WithClock(c clock.Clock)                             // tests; nil is a constructor error

// Lifecycle
func (m *Manager) ActiveSeed(ctx context.Context, playerID string) (Seed, error)      // get-or-create; never exposes the server seed
func (m *Manager) Play(ctx context.Context, playerID string) (*Stream, Proof, error)  // THE hot path: atomic nonce consume → derived Stream + Proof
func (m *Manager) SetClientSeed(ctx context.Context, playerID, clientSeed string) (Seed, error)
func (m *Manager) Rotate(ctx context.Context, playerID string) (old Seed, next Seed, error) // reveals old (ServerSeed set), creates fresh committed pair
func (m *Manager) Seed(ctx context.Context, seedID string) (Seed, error)              // verify pages; ServerSeed only set once revealed
```

```go
type Seed struct {
    ID         string
    PlayerID   string
    Commitment string    // hex SHA-256(serverSeed), always set
    ServerSeed []byte    // nil until revealed
    ClientSeed string
    Nonce      uint64    // next unused
    Status     string    // "active" | "revealed"
    Algorithm  string    // "rng/v1"
    CreatedAt  time.Time
    RevealedAt time.Time // zero until revealed
}

type Proof struct { // what the consumer stores on the game round — everything a verify page needs
    SeedID     string
    Commitment string
    ClientSeed string
    Nonce      uint64
    Algorithm  string
}
```

Rules:

- **One active pair per (scope, player)**, enforced by the store (unique constraint / `ErrExists`).
- **`Play`** consumes the nonce atomically (`ConsumeNonce`, one `UPDATE … RETURNING` in pgstore) and returns the pre-increment value in `Proof.Nonce`. No active pair → get-or-create, then consume (self-healing; also heals a rotate that crashed between reveal and create).
- **`Play` on a revealed seed is impossible by construction** — consumption targets the active row only.
- **`SetClientSeed` rotates rather than mutates**: if an active pair exists it is revealed and a fresh pair is created with the new client seed; if none exists the first pair is created with it. Every `(seed pair, nonce)` triple is immutable once played — history only grows.
- **`Rotate`** requires an active pair (`ErrNotFound` otherwise); reveal-then-create is two store ops, not atomic — a crash between them leaves no active pair, healed by the next `ActiveSeed`/`Play`.
- **Verification is stateless**: the player recomputes with `New(revealedSeed, clientSeed, nonce)` and `VerifyCommitment`; a JS reimplementation needs only the doc.go spec.

## Store seam

```go
type Record struct { // storage shape; ServerSeed always present (needed for derivation until reveal)
    ID, Scope, PlayerID string
    ServerSeed          []byte
    ClientSeed          string
    Nonce               uint64 // next unused; ConsumeNonce returns the record with Nonce set to the consumed value
    Status              string
    Algorithm           string // "rng/v1"
    CreatedAt, RevealedAt time.Time
}

type Store interface {
    Active(ctx context.Context, scope, playerID string) (Record, error)       // ErrNotFound
    Create(ctx context.Context, r Record) error                                // ErrExists if an active pair exists for (scope, player)
    ConsumeNonce(ctx context.Context, scope, playerID string) (Record, error)  // atomic: returns Record with the nonce to use, persists Nonce+1; ErrNotFound if no active pair
    Reveal(ctx context.Context, scope, id string) (Record, error)              // active → revealed, sets RevealedAt; ErrNotFound
    Get(ctx context.Context, scope, id string) (Record, error)                 // ErrNotFound
}
```

- **Memory store** ships in-package (map + mutex) for tests/dev.
- **`gaming/rng/pgstore`** — pgx driver, embedded migrations via `migration.Group("rng")`. Table `rng_seeds`: `id text pk, scope text not null default '', player_id text not null, server_seed bytea not null, client_seed text not null, nonce bigint not null default 0, status text not null, algorithm text not null, created_at timestamptz, revealed_at timestamptz`; partial unique index on `(scope, player_id) where status = 'active'`. `ConsumeNonce` is `UPDATE … SET nonce = nonce + 1 WHERE … AND status = 'active' RETURNING …, nonce - 1 AS consumed` (Postgres `RETURNING` sees the post-update row, so the consumed value is `nonce - 1`).
- **Sensitivity note (doc.go):** rows hold the plaintext server seed until reveal — inherent to commit–reveal. The table must be treated as secret material; at-rest encryption is the consumer's storage concern (disk/pgcrypto), consistent with session stores.

## Tenancy

Standard forge rule: optional construction-time `WithScope(ctx)` hook, fail closed, zero ceremony single-tenant.

- No hook configured → constant scope `""` on every row.
- Hook configured → resolved per call; a hook error or **empty scope fails the call** (`ErrNoScope`) — unlike magiclink there is no legitimate "global" seed pair, every pair belongs to a player within a tenant.
- Scope is threaded through every `Store` method; stores match exactly.

## Errors

`errors.Is`-matchable single-line sentinels in `errors.go`:

- `ErrInvalidSeed` — server seed not exactly 32 bytes.
- `ErrInvalidClientSeed` — length/alphabet violation.
- `ErrInvalidTable` — empty table, zero weight, duplicate/empty key, weight-sum overflow, bad pity config (wrapped with the specific reason).
- `ErrNotFound` — unknown seed ID / no active pair where one is required.
- `ErrExists` — store contract: active pair already exists (consumed internally by Manager's get-or-create; exported for store implementers).
- `ErrNoScope` — fail-closed tenancy.
- `ErrStore` — wraps driver failures (cache precedent).

Constructor misuse (nil store/hook/clock) returns plain errors from `NewManager` via `errors.Join`.

## Package anatomy

`gaming/rng` (~700 LOC) + `gaming/rng/pgstore` driver leaf:

- `doc.go` — runnable example (casual lootbox + provably-fair slots round + verification), the normative `rng/v1` spec, sensitivity + anti-scope notes.
- `stream.go` — Stream, derivation, draws. Hot-path discipline: one `hmac` instance per Stream, `Reset()` between blocks, `strconv.AppendUint` message building; zero allocs per draw after construction (benchmark-verified).
- `table.go` — Entry, Table, pity.
- `cards.go` — Card, NewDeck, Deal.
- `manager.go` — Manager, Seed, Proof.
- `store.go` — Store interface, Record; `memory.go` — memory store.
- `options.go`, `errors.go`, black-box tests, `bench_test.go`.

## Usage examples (abridged; full version in doc.go)

Casual lootbox (SaaS gamification, no verifiability ceremony):

```go
table, err := rng.NewTable([]rng.Entry[Reward]{
    {Key: "common",    Weight: 700, Item: Reward{Coins: 10}},
    {Key: "rare",      Weight: 250, Item: Reward{Coins: 100}},
    {Key: "legendary", Weight: 50,  Item: Reward{Skin: "dragon"}},
}, rng.WithPity(90, "legendary"))

entry, misses := table.PickWithPity(rng.Casual(), player.PityMisses) // caller persists misses
openRecord.TableVersion = table.Version()                            // which drop config was live
```

Provably-fair slots:

```go
m, err := rng.NewManager(pgstore.New(pool)) // + rng.WithScope(tenantFromCtx) if multi-tenant

seed, _ := m.ActiveSeed(ctx, playerID)      // seed.Commitment → fairness panel, before any bet

stream, proof, err := m.Play(ctx, playerID) // atomic nonce consume
stops := stream.Ints(5, len(reelStrip))     // 5 reels from one nonce
// consumer: payout math, persist round + proof, settle via ledger in its own tx

old, _, _ := m.Rotate(ctx, playerID)        // old.ServerSeed now revealed
s, _ := rng.New(old.ServerSeed, proof.ClientSeed, proof.Nonce)
same := s.Ints(5, len(reelStrip))           // == stops; JS verifier reproduces from the doc.go spec
```

## Testing

Black-box (`package rng_test`):

- **Golden vectors** — frozen hex expectations for the full derivation chain; vectors cross-checked in-test against an independent naive reimplementation.
- **Statistical sanity** — deterministic-seed chi-squared-style bounds on IntN/Pick/Shuffle uniformity (loose tolerances, zero flakiness).
- **Fuzz** — arbitrary seeds/weights/params: no panics, IntN always in `[0, n)`, rejection sampling terminates, NewTable never accepts invalid input.
- **Store contract suite** shared by memory + pgstore (apikey precedent; pgstore against real Postgres in CI). Concurrency: parallel `Play` on one player yields strictly unique nonces.
- **Pity** — forced hit exactly at threshold, reset on natural hit, counter increments otherwise, replay determinism.
- **Manager lifecycle** — get-or-create, rotate reveals + fresh commitment, `SetClientSeed` rotation, `Play` after crash-between-rotate-ops heals, commitment verification round-trip, scope matrix (fail-closed).
- **Benchmarks** (`bench_test.go`, repo rule): `Uint64`, `IntN`, `Float64`, `Ints(5)`, `Table.Pick`, `Manager.Play` on memory store — zero-alloc target for Stream draws; before/after numbers in the PR.

## Anti-scope

No game math (paylines, RTP, payout multipliers — the demo-games product owns those); no certified-RNG claims (GLI-19 is lab certification of deployments, not code — doc.go says so explicitly); no pity-counter storage; no jackpot/tournament logic; no bet/wallet integration (compose with `finance/ledger`); no seed encryption at rest (documented storage concern); no shipped cross-language verifier (the doc.go spec is the contract; a JS example may land in `examples/` later).

## Decisions log

| Decision | Choice | Why |
|----------|--------|-----|
| Trust model | Layered: one deterministic core, casual + provably-fair entry points | Mechanics math is identical; only the entropy source differs |
| Derivation | Own spec'd algorithm (`rng/v1`), frozen, versioned | Third-party verifiability forever; `math/rand/v2` internals are not a contract |
| Seed lifecycle | Full Manager over a Store seam (user decision) | Consumers get commit–reveal correct by construction |
| IntN | Rejection sampling on Uint64 | No modulo bias; portable to JS/BigInt |
| Multi-value per nonce | Sequential stream consumption | No extra parameters; verifier replays draws in order |
| SetClientSeed | Rotates the pair, never mutates | Played (pair, nonce) triples stay immutable — history only grows |
| Pity | Pure function, caller persists the counter | Counter must update atomically with the reward grant — only the consumer's tx can |
| Pick returns | Full `Entry[T]` | Key needed for audit logs without a reverse lookup |
| Table identity | `Version()` = SHA-256 over ordered (key, weight) | Dispute resolution: prove which drop config was live |
| Client seed alphabet | `[A-Za-z0-9_-]`, 1–64 chars | Unambiguous HMAC messages; copy-paste-safe verify pages |
| Tenancy | Fail-closed `WithScope`; empty scope from a configured hook is an error | Seeds are always player-owned; no global case exists |
| Placement | New `gaming/` domain, leaf `rng` | Admission test passes; iGaming direction implies future siblings; `rng` is an industry-standard acronym |
