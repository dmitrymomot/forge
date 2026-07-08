package useragent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseBots(t *testing.T) {
	tests := []struct {
		name     string
		ua       string
		botName  string
		category useragent.BotCategory
		operator string
	}{
		{"googlebot short-circuits chrome", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; Googlebot/2.1; +http://www.google.com/bot.html) Chrome/126.0.0.0 Safari/537.36", "Googlebot", useragent.BotSearchEngine, "Google"},
		{"gptbot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; GPTBot/1.2; +https://openai.com/gptbot", "GPTBot", useragent.BotAICrawler, "OpenAI"},
		{"claudebot", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0; +claudebot@anthropic.com)", "ClaudeBot", useragent.BotAICrawler, "Anthropic"},
		{"chatgpt agent", "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot", "ChatGPT-User", useragent.BotAIAgent, "OpenAI"},
		{"slack preview", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", "Slackbot", useragent.BotSocialPreview, "Slack"},
		{"whatsapp preview", "WhatsApp/2.23.24.76 A", "WhatsApp Preview", useragent.BotSocialPreview, "Meta"},
		{"uptimerobot", "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)", "UptimeRobot", useragent.BotMonitoring, "UptimeRobot"},
		{"ahrefs", "Mozilla/5.0 (compatible; AhrefsBot/7.0; +http://ahrefs.com/robot/)", "AhrefsBot", useragent.BotSEO, "Ahrefs"},
		{"adsbot", "AdsBot-Google (+http://www.google.com/adsbot.html)", "AdsBot", useragent.BotAdvertising, "Google"},
		{"censys", "Mozilla/5.0 (compatible; CensysInspect/1.1; +https://about.censys.io/)", "CensysInspect", useragent.BotSecurity, "Censys"},
		{"stripe webhook", "Stripe/1.0 (+https://stripe.com/docs/webhooks)", "Stripe Webhook", useragent.BotWebhook, "Stripe"},
		{"feedly", "Feedly/1.0 (+http://www.feedly.com/fetcher.html; 16 subscribers)", "Feedly", useragent.BotFeedFetcher, "Feedly"},
		{"scrapy", "Scrapy/2.11.2 (+https://scrapy.org)", "Scrapy", useragent.BotScraper, ""},
		{"curl", "curl/8.6.0", "curl", useragent.BotLibrary, ""},
		{"python requests", "python-requests/2.31.0", "python-requests", useragent.BotLibrary, ""},
		{"go http client", "Go-http-client/2.0", "Go-http-client", useragent.BotLibrary, ""},
		{"generic via url", "Zzqx/1.0 (+https://zzqx.invalid/info)", "", useragent.BotUnknown, ""},
		{"generic via bot suffix", "Zzqxbot/1.0", "", useragent.BotUnknown, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			require.True(t, got.IsBot(), "must be a bot")
			assert.Equal(t, useragent.DeviceBot, got.Device.Type)
			assert.Equal(t, tt.botName, got.Bot.Name)
			assert.Equal(t, tt.category, got.Bot.Category)
			assert.Equal(t, tt.operator, got.Bot.Operator)
			assert.Empty(t, got.Browser.Name, "browser stays zero for bots")
			assert.Empty(t, got.OS.Name, "os stays zero for bots")
		})
	}
}

func TestNotBots(t *testing.T) {
	for name, ua := range map[string]string{
		"chrome":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
		"cubot phone":    "Mozilla/5.0 (Linux; Android 13; Cubot KingKong 9 Build/TP1A.220624.014) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36",
		"empty":          "",
		"abbott not bot": "Mozilla/5.0 (compatible; Abbott Healthcare App) AppleWebKit/537.36",
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, useragent.Parse(ua).IsBot())
		})
	}
}

// Every curated entry must be detectable and correctly attributed via a
// synthetic UA containing its token.
func TestCuratedBotsAllDetected(t *testing.T) {
	require.NotEmpty(t, useragent.CuratedBots)
	for _, e := range useragent.CuratedBots {
		ua := "Mozilla/5.0 (compatible) " + e.Token + "1.0"
		got := useragent.Parse(ua)
		require.True(t, got.IsBot(), "token %q", e.Token)
		assert.Equal(t, e.Name, got.Bot.Name, "token %q", e.Token)
		assert.Equal(t, e.Category, got.Bot.Category, "token %q", e.Token)
		assert.Equal(t, e.Operator, got.Bot.Operator, "token %q", e.Token)
	}
}

// The trigger gate must make every curated token reachable.
func TestCuratedBotsReachableViaTriggers(t *testing.T) {
	for _, e := range useragent.CuratedBots {
		reachable := false
		for _, trig := range useragent.BotTriggers {
			if strings.Contains(e.Token, trig) {
				reachable = true
				break
			}
		}
		assert.True(t, reachable, "curated token %q contains no trigger — detectBot can never reach it", e.Token)
	}
}
