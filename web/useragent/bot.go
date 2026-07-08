package useragent

import (
	"slices"
	"strings"
)

// botTriggers is a cheap gate: the full bot tables are scanned only when the
// UA contains one of these substrings. Every curated token and every
// generated pattern MUST contain at least one trigger (invariant-tested and
// enforced by the generator), otherwise the entry is unreachable.
var botTriggers = []string{
	"bot", "crawl", "spider", "scan", "search", "slurp", "monitor",
	"archive", "fetch", "http", "index", "feed", "agent", "headless",
	"preview", "validator", "python", "java", "curl", "wget", "node",
	"axios", "okhttp", "go-http", "scrapy", "phantom", "gpt", "claude",
	"perplexity", "whatsapp", "facebookexternalhit", "pingdom",
	"statuscake", "censys", "expanse", "mediapartners", "stripe/",
	"paypal", "libwww", "track",
}

// botEntry fields are exported so export_test.go can re-export the table
// for invariant tests; the type itself stays unexported.
type botEntry struct {
	Token    string // lowercase substring
	Name     string
	Category BotCategory
	Operator string
}

var curatedBots = []botEntry{
	// search engines
	{"googlebot", "Googlebot", BotSearchEngine, "Google"},
	{"bingbot", "Bingbot", BotSearchEngine, "Microsoft"},
	{"bingpreview", "BingPreview", BotSearchEngine, "Microsoft"},
	{"duckduckbot", "DuckDuckBot", BotSearchEngine, "DuckDuckGo"},
	{"baiduspider", "Baiduspider", BotSearchEngine, "Baidu"},
	{"yandexbot", "YandexBot", BotSearchEngine, "Yandex"},
	{"applebot", "Applebot", BotSearchEngine, "Apple"},
	{"slurp", "Yahoo Slurp", BotSearchEngine, "Yahoo"},
	// AI crawlers
	{"gptbot", "GPTBot", BotAICrawler, "OpenAI"},
	{"oai-searchbot", "OAI-SearchBot", BotAICrawler, "OpenAI"},
	{"claudebot", "ClaudeBot", BotAICrawler, "Anthropic"},
	{"perplexitybot", "PerplexityBot", BotAICrawler, "Perplexity"},
	{"bytespider", "Bytespider", BotAICrawler, "ByteDance"},
	{"ccbot", "CCBot", BotAICrawler, "Common Crawl"},
	{"meta-externalagent", "Meta-ExternalAgent", BotAICrawler, "Meta"},
	// AI agents (user-triggered fetches)
	{"chatgpt-user", "ChatGPT-User", BotAIAgent, "OpenAI"},
	{"claude-user", "Claude-User", BotAIAgent, "Anthropic"},
	{"perplexity-user", "Perplexity-User", BotAIAgent, "Perplexity"},
	// social link previews
	{"facebookexternalhit", "Facebook Preview", BotSocialPreview, "Meta"},
	{"twitterbot", "Twitterbot", BotSocialPreview, "X"},
	{"slackbot", "Slackbot", BotSocialPreview, "Slack"},
	{"whatsapp", "WhatsApp Preview", BotSocialPreview, "Meta"},
	{"discordbot", "Discordbot", BotSocialPreview, "Discord"},
	{"telegrambot", "TelegramBot", BotSocialPreview, "Telegram"},
	{"linkedinbot", "LinkedInBot", BotSocialPreview, "LinkedIn"},
	// monitoring
	{"uptimerobot", "UptimeRobot", BotMonitoring, "UptimeRobot"},
	{"pingdom", "Pingdom", BotMonitoring, "Pingdom"},
	{"statuscake", "StatusCake", BotMonitoring, "StatusCake"},
	// SEO
	{"ahrefsbot", "AhrefsBot", BotSEO, "Ahrefs"},
	{"semrushbot", "SemrushBot", BotSEO, "Semrush"},
	{"mj12bot", "MJ12bot", BotSEO, "Majestic"},
	{"dotbot", "DotBot", BotSEO, "Moz"},
	{"screaming frog seo spider", "Screaming Frog", BotSEO, "Screaming Frog"},
	// advertising
	{"adsbot-google", "AdsBot", BotAdvertising, "Google"},
	{"mediapartners-google", "Mediapartners", BotAdvertising, "Google"},
	{"adidxbot", "AdIdxBot", BotAdvertising, "Microsoft"},
	// security scanners
	{"censysinspect", "CensysInspect", BotSecurity, "Censys"},
	{"expanse", "Expanse", BotSecurity, "Palo Alto Networks"},
	// webhooks
	{"stripe/", "Stripe Webhook", BotWebhook, "Stripe"},
	{"paypal ipn", "PayPal IPN", BotWebhook, "PayPal"},
	// feed fetchers
	{"feedfetcher", "FeedFetcher", BotFeedFetcher, "Google"},
	{"feedly", "Feedly", BotFeedFetcher, "Feedly"},
	// scrapers
	{"scrapy", "Scrapy", BotScraper, ""},
	{"httrack", "HTTrack", BotScraper, ""},
	// HTTP libraries
	{"curl/", "curl", BotLibrary, ""},
	{"wget/", "Wget", BotLibrary, ""},
	{"python-requests", "python-requests", BotLibrary, ""},
	{"python-urllib", "python-urllib", BotLibrary, ""},
	{"python-httpx", "python-httpx", BotLibrary, ""},
	{"go-http-client", "Go-http-client", BotLibrary, ""},
	{"okhttp", "okhttp", BotLibrary, ""},
	{"java/", "Java", BotLibrary, ""},
	{"axios/", "axios", BotLibrary, ""},
	{"node-fetch", "node-fetch", BotLibrary, ""},
	{"libwww-perl", "libwww-perl", BotLibrary, ""},
	{"guzzlehttp", "Guzzle", BotLibrary, ""},
}

// notBotTokens are device names that end in "bot" and would otherwise trip
// the generic suffix heuristic (Cubot phones).
var notBotTokens = []string{"cubot"}

// detectBot layers: trigger gate → curated table → generated table →
// generic heuristics. Curated wins over generated on conflict because it is
// checked first.
func detectBot(in input) (Bot, bool) {
	if !slices.ContainsFunc(botTriggers, in.contains) {
		return Bot{}, false
	}
	for _, e := range curatedBots {
		if in.contains(e.Token) {
			return Bot{Name: e.Name, Category: e.Category, Operator: e.Operator}, true
		}
	}
	for _, p := range generatedBotPatterns {
		if in.contains(p) {
			return Bot{Name: botNameFromPattern(p), Category: BotUnknown}, true
		}
	}
	if genericBot(in) {
		return Bot{Category: BotUnknown}, true
	}
	return Bot{}, false
}

func genericBot(in input) bool {
	if in.contains("+http://") || in.contains("+https://") {
		return true
	}
	if in.contains("crawler") || in.contains("spider") || in.contains("scanner") {
		return true
	}
	if hasBotSuffix(in.lower) {
		return !slices.ContainsFunc(notBotTokens, in.contains)
	}
	return false
}

// hasBotSuffix reports a "bot" occurrence followed by a non-letter or end of
// string ("Googlebot/", "Zzqxbot") without matching mid-word hits
// ("Abbott" → followed by 't').
func hasBotSuffix(b []byte) bool {
	for i := 0; i+3 <= len(b); i++ {
		if b[i] != 'b' || b[i+1] != 'o' || b[i+2] != 't' {
			continue
		}
		if i+3 == len(b) {
			return true
		}
		if c := b[i+3]; c < 'a' || c > 'z' {
			return true
		}
	}
	return false
}

// botNameFromPattern turns a generated dataset pattern into a display name.
func botNameFromPattern(p string) string {
	return strings.Trim(p, "/-. ")
}
