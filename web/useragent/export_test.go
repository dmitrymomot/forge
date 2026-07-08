package useragent

// Test-only shims: invariant tests assert internal table state (the one
// permitted white-box use). Behavior tests stay black-box in useragent_test.
type BotEntry = botEntry

var (
	CuratedBots          = curatedBots
	BotTriggers          = botTriggers
	GeneratedBotPatterns = generatedBotPatterns
)
