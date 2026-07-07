// Package featureflag provides standalone feature flags: bool/variant values
// with typed getters, deterministic percentage rollout, token-based
// allow/deny targeting, and pluggable flag storage behind a one-method
// Provider seam. A flag is a small serializable data record; all evaluation
// (enabled → deny → allow → rollout) happens in this package, so every
// backend — config file, memory, your own database — behaves identically.
//
// # Micro-SaaS floor (config only)
//
// Embed Flags in the app config (ops/config), one option, done:
//
//	// config/production.yaml:
//	//   flags:
//	//     dark_mode: true
//	//     new_editor: { value: true, rollout: 25 }
//	type AppConfig struct {
//		Flags featureflag.Flags `yaml:"flags"`
//	}
//
//	flags, err := featureflag.New(featureflag.WithFlags(cfg.Flags))
//	if flags.Bool(r.Context(), "new_editor", false) { /* new path */ }
//
// Getters never error: missing key, disabled flag, provider failure, lost
// rollout bucket, or a malformed value all return the caller's default
// (WithLogger surfaces the latter two as WARN records).
//
// # Subjects, rollout, and identity tokens
//
// Percentage rollout buckets on a stable subject ID set once per request by
// auth middleware; identity tokens make populations targetable in flag data:
//
//	ctx = featureflag.WithSubject(ctx, user.ID) // IDs should be globally unique (id.Prefix style)
//
//	flags, _ := featureflag.New(
//		featureflag.WithFlags(cfg.Flags),
//		// tokens must be O(1) from pre-loaded request state — never a DB call;
//		// compute segment membership at session create/refresh
//		featureflag.WithIdentity(func(ctx context.Context) []string {
//			return session.From(ctx).Tokens // ["role:staff", "segment:vip"]
//		}),
//	)
//
// A flag with rollout 0 and allow [role:staff] is the dogfooding pattern:
// live in production for the team, invisible to users. Raising a rollout
// percent never flips existing subjects off (FNV bucketing is monotonic).
// Jobs and CLIs without request context bind the subject explicitly:
//
//	if flags.For(job.UserID, "segment:vip").Bool("new_payout", false) { ... }
//
// # Runtime toggles without infrastructure
//
// The Memory provider is mutable — a guarded admin handler makes a
// maintenance gate (the recipe formerly owed by docs/packages.md):
//
//	mem := featureflag.NewMemory(nil)
//	flags, _ := featureflag.New(featureflag.WithProvider(mem), featureflag.WithFlags(cfg.Flags))
//
//	// admin handler behind auth/guard:
//	_ = mem.Set("maintenance", featureflag.Flag{Value: "true", Enabled: true, Rollout: 100})
//
//	// middleware:
//	if flags.Bool(r.Context(), "maintenance", false) {
//		problem.ServiceUnavailable("maintenance in progress").Write(w, r)
//		return
//	}
//
// # Database-backed flags (multi-tenant recipe)
//
// A Provider is one method; ctx carries the tenant. Wrap it in Cached —
// cache cardinality is tenants × flags, never users, and a failed refresh
// serves the last-known value (singleflight collapses refresh stampedes):
//
//	// CREATE TABLE feature_flags (
//	//     tenant_id     text        NOT NULL,
//	//     key           text        NOT NULL,
//	//     value         text        NOT NULL DEFAULT 'true',
//	//     enabled       boolean     NOT NULL DEFAULT true,
//	//     rollout       smallint    NOT NULL DEFAULT 100 CHECK (rollout BETWEEN 0 AND 100),
//	//     allow_tokens  text[]      NOT NULL DEFAULT '{}',
//	//     deny_tokens   text[]      NOT NULL DEFAULT '{}',
//	//     has_overrides boolean     NOT NULL DEFAULT false,
//	//     updated_at    timestamptz NOT NULL DEFAULT now(),
//	//     PRIMARY KEY (tenant_id, key)
//	// );
//	// -- individual force on/off at scale, consulted only when has_overrides:
//	// CREATE TABLE feature_flag_overrides (
//	//     tenant_id text NOT NULL, key text NOT NULL, subject_id text NOT NULL,
//	//     allow boolean NOT NULL, PRIMARY KEY (tenant_id, key, subject_id)
//	// );
//
//	type pgFlags struct{ db *pgxpool.Pool }
//
//	func (p *pgFlags) Flag(ctx context.Context, key string) (featureflag.Flag, bool, error) {
//		var f featureflag.Flag
//		err := p.db.QueryRow(ctx,
//			`SELECT value, enabled, rollout, allow_tokens, deny_tokens
//			   FROM feature_flags WHERE tenant_id = $1 AND key = $2`,
//			tenant.From(ctx), key,
//		).Scan(&f.Value, &f.Enabled, &f.Rollout, &f.Allow, &f.Deny)
//		if errors.Is(err, pgx.ErrNoRows) {
//			return featureflag.Flag{}, false, nil // miss → next layer (config defaults)
//		}
//		return f, err == nil, err
//	}
//
//	flags, _ := featureflag.New(
//		featureflag.WithProvider(featureflag.Cached(&pgFlags{db}, 30*time.Second,
//			featureflag.CacheKey(func(ctx context.Context) string { return tenant.From(ctx) }))),
//		featureflag.WithFlags(cfg.Flags), // platform defaults under the DB layer
//		featureflag.WithIdentity(identityTokens),
//	)
//
// Discipline: tokens for populations (segment:vip), the overrides table for
// individuals, array columns only for handfuls. Tenant-facing flag
// management (a catalog gating which flags operators may touch, validated
// CRUD over these tables) is an application feature — this package only
// reads; see the design doc for the full back-office recipe.
//
// # Providers and precedence
//
// New's option order is the lookup chain, first hit wins; the static set
// occupies the position of the first static option. A provider error is
// logged and treated as a miss for that provider, so a database outage
// degrades to config defaults, not to hardcoded call-site defaults.
// Client.All merges Lister providers with the same precedence for a debug
// endpoint or admin page.
package featureflag
