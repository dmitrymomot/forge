package rng

import "strconv"

// Suit is a playing-card suit.
type Suit uint8

// Suits in canonical deck order.
const (
	Spades Suit = iota
	Hearts
	Diamonds
	Clubs
)

var suitSymbols = [...]string{"♠", "♥", "♦", "♣"}

// String returns the suit symbol.
func (s Suit) String() string {
	if int(s) >= len(suitSymbols) {
		return "?"
	}
	return suitSymbols[s]
}

// Card is one playing card: value = suit*13 + rankIndex, where rankIndex
// 0 is Two and 12 is Ace. Values repeat across decks from NewDeck.
type Card uint8

// Suit returns the card's suit.
func (c Card) Suit() Suit { return Suit(c / 13 % 4) }

// Rank returns the rank as 2-14 (11 = Jack, 12 = Queen, 13 = King, 14 = Ace).
func (c Card) Rank() int { return int(c%13) + 2 }

// String renders like "A♠" or "10♥".
func (c Card) String() string {
	var rank string
	switch r := c % 13; {
	case r <= 8:
		rank = strconv.Itoa(int(r) + 2)
	case r == 9:
		rank = "J"
	case r == 10:
		rank = "Q"
	case r == 11:
		rank = "K"
	default:
		rank = "A"
	}
	return rank + c.Suit().String()
}

// NewDeck returns decks standard 52-card decks in canonical order
// (Spades Two..Ace, Hearts, Diamonds, Clubs, repeated per deck). It
// panics if decks <= 0.
func NewDeck(decks int) []Card {
	if decks <= 0 {
		panic("rng: NewDeck decks must be > 0")
	}
	out := make([]Card, 0, decks*52)
	for range decks {
		for c := range 52 {
			out = append(out, Card(c))
		}
	}
	return out
}

// Deal draws n items without replacement from a copy of items (cards,
// raffle winners): n partial Fisher-Yates steps in stream order, dealt
// items returned in draw order; items is never mutated. Deal(s, items,
// len(items)) consumes exactly the draws of Shuffle(len(items)). It
// panics if n < 0 or n > len(items).
func Deal[T any](s *Stream, items []T, n int) []T {
	if n < 0 || n > len(items) {
		panic("rng: Deal n must be in [0, len(items)]")
	}
	cp := append([]T(nil), items...)
	out := make([]T, 0, n)
	for i := len(cp) - 1; i > 0 && len(out) < n; i-- {
		j := s.IntN(i + 1)
		cp[i], cp[j] = cp[j], cp[i]
		out = append(out, cp[i])
	}
	if len(out) < n { // n == len(items): the final untouched element
		out = append(out, cp[0])
	}
	return out
}
