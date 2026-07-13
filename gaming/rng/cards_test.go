package rng_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/gaming/rng"
)

func TestCard_StringRankSuit(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	require.Len(t, deck, 52)
	assert.Equal(t, "2♠", deck[0].String())
	assert.Equal(t, "10♠", deck[8].String())
	assert.Equal(t, "J♠", deck[9].String())
	assert.Equal(t, "A♠", deck[12].String())
	assert.Equal(t, "2♥", deck[13].String())
	assert.Equal(t, "A♣", deck[51].String())

	assert.Equal(t, 2, deck[0].Rank())
	assert.Equal(t, 14, deck[12].Rank()) // Ace
	assert.Equal(t, rng.Spades, deck[0].Suit())
	assert.Equal(t, rng.Clubs, deck[51].Suit())
}

func TestNewDeck_MultiDeck(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(2)
	require.Len(t, deck, 104)
	counts := map[rng.Card]int{}
	for _, c := range deck {
		counts[c]++
	}
	require.Len(t, counts, 52)
	for c, n := range counts {
		assert.Equal(t, 2, n, "card %s", c)
	}
	assert.Panics(t, func() { rng.NewDeck(0) })
}

func TestDeal_Deterministic(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	s1, err := rng.New(testSeed(), "cards", 0)
	require.NoError(t, err)
	s2, err := rng.New(testSeed(), "cards", 0)
	require.NoError(t, err)
	h1 := rng.Deal(s1, deck, 5)
	h2 := rng.Deal(s2, deck, 5)
	assert.Equal(t, h1, h2)
	require.Len(t, h1, 5)
}

func TestDeal_WithoutReplacementAndNoMutation(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	orig := append([]rng.Card(nil), deck...)
	hand := rng.Deal(rng.Casual(), deck, 52)
	assert.Equal(t, orig, deck) // input never mutated
	seen := map[rng.Card]bool{}
	for _, c := range hand {
		assert.False(t, seen[c], "duplicate card %s", c)
		seen[c] = true
	}
	assert.Len(t, seen, 52)
}

func TestDeal_Panics(t *testing.T) {
	t.Parallel()
	deck := rng.NewDeck(1)
	assert.Panics(t, func() { rng.Deal(rng.Casual(), deck, -1) })
	assert.Panics(t, func() { rng.Deal(rng.Casual(), deck, 53) })
	assert.Empty(t, rng.Deal(rng.Casual(), deck, 0))
}

func TestDeal_GenericRaffle(t *testing.T) {
	t.Parallel()
	users := []string{"ann", "bob", "cee", "dan", "eve"}
	winners := rng.Deal(rng.Casual(), users, 2)
	require.Len(t, winners, 2)
	assert.NotEqual(t, winners[0], winners[1])
}
