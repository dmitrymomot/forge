package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestGeneratedPatternsWellFormed(t *testing.T) {
	require.NotEmpty(t, useragent.GeneratedBotPatterns, "run go run ./web/useragent/gen first")
	prev := ""
	for _, p := range useragent.GeneratedBotPatterns {
		assert.Equal(t, strings.ToLower(p), p, "patterns must be lowercase")
		assert.GreaterOrEqual(t, len(p), 5, "pattern %q too short", p)
		assert.False(t, strings.ContainsAny(p, `\^$|?*+()[]{}`), "regex leftovers in %q", p)
		assert.Less(t, prev, p, "table must be sorted and deduped")
		prev = p
	}
}

// Every generated pattern must be reachable through the runtime trigger
// gate — this catches drift between gen's trigger copy and bot.go's.
func TestGeneratedPatternsReachable(t *testing.T) {
	for _, p := range useragent.GeneratedBotPatterns {
		reachable := false
		for _, trig := range useragent.BotTriggers {
			if strings.Contains(p, trig) {
				reachable = true
				break
			}
		}
		assert.True(t, reachable, "generated pattern %q contains no bot.go trigger", p)
	}
}

// End-to-end: a UA embedding any generated pattern parses as a bot.
func TestGeneratedPatternsDetected(t *testing.T) {
	for _, p := range useragent.GeneratedBotPatterns {
		got := useragent.Parse("Mozilla/5.0 (compatible) " + p)
		assert.True(t, got.IsBot(), "pattern %q", p)
	}
}

// Curated attribution wins over the generated table.
func TestCuratedPrecedesGenerated(t *testing.T) {
	got := useragent.Parse("Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	assert.Equal(t, "Googlebot", got.Bot.Name)
	assert.Equal(t, useragent.BotSearchEngine, got.Bot.Category)
}
