// Package rng provides deterministic random outcomes for game mechanics:
// weighted drop tables (lootbox, wheel), dice, cards, slot-reel values —
// over one explicitly specified derivation algorithm with two entry
// points: Casual (CSPRNG, zero ceremony) and a provably-fair seed-chain
// Manager (commit-reveal, Store seam).
//
// # The rng/v1 derivation spec (normative, frozen)
//
// All randomness flows through a Stream derived from (serverSeed,
// clientSeed, nonce). The algorithm below is frozen forever under the
// identifier "rng/v1" (the Algorithm constant, stamped into every Proof
// and seed record); any change ships as rng/v2 alongside so old outcomes
// stay verifiable. It is reproducible in any language:
//
//   - Server seed: exactly 32 bytes. Commitment: lowercase-hex
//     SHA-256(serverSeed), published before play.
//   - Client seed: 1-64 chars of [A-Za-z0-9_-].
//   - Nonce: uint64 round counter, starting at 0.
//   - Block expansion: block_i = HMAC-SHA256(key = serverSeed,
//     msg = clientSeed || ":" || decimal(nonce) || ":" || decimal(i)) for
//     i = 0, 1, 2, ... The stream serves these 32-byte blocks
//     sequentially; each draw consumes exactly the bytes it needs,
//     crossing block boundaries transparently. A verifier replays draws
//     in order.
//   - Uint64: next 8 bytes, big-endian.
//   - IntN(n): rejection sampling — draw Uint64 until
//     v < 2^64 - (2^64 mod n), return v mod n. No modulo bias.
//   - Float64: (Uint64 >> 11) / 2^53, in [0, 1).
//   - Shuffle(n): Fisher-Yates: for i = n-1 down to 1, j = IntN(i+1),
//     swap(i, j). Perm(n) is the identity slice reordered by Shuffle.
//     Deal performs the same steps, stopping after n draws and returning
//     dealt items in draw order.
//   - Weighted pick (Table): draw IntN(total weight); the first entry, in
//     table order, whose cumulative weight exceeds the draw wins.
//
// # Casual use — lootbox with pity
//
//	table, err := rng.NewTable([]rng.Entry[Reward]{
//		{Key: "common", Weight: 700, Item: rewardCoins},
//		{Key: "rare", Weight: 250, Item: rewardGem},
//		{Key: "legendary", Weight: 50, Item: rewardSkin},
//	}, rng.WithPity(90, "legendary"))
//	if err != nil {
//		panic(err)
//	}
//	entry, misses := table.PickWithPity(rng.Casual(), player.PityMisses)
//	player.PityMisses = misses          // persist next to the player row
//	audit.TableVersion = table.Version() // prove which config was live
//
// The pity counter lives in the consumer's database, never here: it must
// update atomically with granting the reward, and only the consumer's
// transaction can do that.
//
// # Provably fair — slots round
//
//	store := pgstore.New(pool) // or rng.NewMemoryStore() for tests
//	m, err := rng.NewManager(store)
//	if err != nil {
//		panic(err)
//	}
//
//	seed, _ := m.ActiveSeed(ctx, playerID) // seed.Commitment → fairness UI, before any bet
//	stream, proof, err := m.Play(ctx, playerID)
//	if err != nil {
//		return err
//	}
//	stops := stream.Ints(5, len(reelStrip)) // 5 reels from one nonce
//	// Persist the round with proof; settle the bet in the consumer's tx.
//
// Verification after rotation:
//
//	old, _, _ := m.Rotate(ctx, playerID) // old.ServerSeed now revealed
//	ok := rng.VerifyCommitment(old.ServerSeed, proof.Commitment)
//	replay, _ := rng.New(old.ServerSeed, proof.ClientSeed, proof.Nonce)
//	same := replay.Ints(5, len(reelStrip)) // == stops
//
// Multi-tenant apps add rng.WithScope(func(ctx) (string, error)) —
// fail-closed: a hook error or empty scope fails the call with
// ErrNoScope. Single-tenant apps configure nothing.
//
// # Security and operational notes
//
// Seed records hold the plaintext server seed until reveal — inherent to
// commit-reveal. Treat the store as secret material; at-rest encryption
// is the consumer's storage concern. The server necessarily knows future
// outcomes for the active pair: provably fair proves non-manipulation
// after the fact, not server ignorance — which is why players can change
// their client seed (SetClientSeed rotates the pair) at any time.
//
// This package makes no certified-RNG claims: GLI-19 and similar are lab
// certifications of deployed systems, not properties a library can
// grant. Game math — paylines, RTP, payout multipliers — bet handling,
// and wallets are out of scope; compose with finance/ledger for money.
package rng
