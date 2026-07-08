package useragent

import (
	"strconv"
	"strings"
)

// DeviceType classifies the hardware class a user agent runs on.
type DeviceType string

const (
	DeviceUnknown  DeviceType = "unknown"
	DeviceDesktop  DeviceType = "desktop"
	DeviceMobile   DeviceType = "mobile"
	DeviceTablet   DeviceType = "tablet"
	DeviceTV       DeviceType = "tv"
	DeviceConsole  DeviceType = "console"
	DeviceWearable DeviceType = "wearable"
	DeviceEReader  DeviceType = "ereader"
	DeviceBot      DeviceType = "bot"
)

// BotCategory classifies what an automated agent is for.
type BotCategory string

const (
	BotUnknown       BotCategory = "unknown"
	BotSearchEngine  BotCategory = "search_engine"
	BotAICrawler     BotCategory = "ai_crawler"
	BotAIAgent       BotCategory = "ai_agent"
	BotSocialPreview BotCategory = "social_preview"
	BotMonitoring    BotCategory = "monitoring"
	BotScraper       BotCategory = "scraper"
	BotSEO           BotCategory = "seo"
	BotAdvertising   BotCategory = "advertising"
	BotSecurity      BotCategory = "security"
	BotWebhook       BotCategory = "webhook"
	BotFeedFetcher   BotCategory = "feed_fetcher"
	BotLibrary       BotCategory = "library"
)

// Version holds a parsed dotted version. Full preserves everything that was
// matched (e.g. "138.0.7204.97"); Major/Minor/Patch hold the first three
// numeric segments.
type Version struct {
	Full                string
	Major, Minor, Patch int
}

// IsZero reports whether no version was detected.
func (v Version) IsZero() bool { return v.Full == "" }

// Browser identifies the browser or embedded webview.
type Browser struct {
	Name    string
	Version Version
}

// OS identifies the operating system with marketing versioning
// (Windows NT 6.3 parses as "8.1").
type OS struct {
	Name    string
	Version Version
}

// Device identifies the hardware class and, when the UA exposes it,
// the vendor and model.
type Device struct {
	Type         DeviceType
	Brand, Model string
}

// Bot identifies an automated agent. Zero value unless IsBot().
type Bot struct {
	Name     string
	Category BotCategory
	Operator string
}

// UserAgent is the parsed result. Zero fields mean "not detected" —
// parsing never fails.
type UserAgent struct {
	Device  Device
	Bot     Bot
	Raw     string
	Browser Browser
	OS      OS
}

// IsBot reports whether the user agent is an automated client.
func (ua UserAgent) IsBot() bool { return ua.Device.Type == DeviceBot }

// Parse extracts browser, OS, device, and bot facts from a raw User-Agent
// string. It never returns an error and never panics; unrecognized input
// yields zero values with Device.Type == DeviceUnknown.
func Parse(ua string) UserAgent {
	res := UserAgent{Raw: ua, Device: Device{Type: DeviceUnknown}}
	if ua == "" {
		return res
	}
	var buf [512]byte
	in := newInput(ua, buf[:0])
	if bot, ok := detectBot(in); ok {
		res.Bot = bot
		res.Device.Type = DeviceBot
		return res
	}
	res.Browser = detectBrowser(in, ua)
	res.OS = detectOS(in, ua)
	res.Device = detectDevice(in, ua, res.OS.Name)
	if res.Device.Brand == "Amazon" && res.OS.Name == "Android" &&
		(res.Device.Type == DeviceTablet || res.Device.Type == DeviceTV) {
		res.OS = OS{Name: "Fire OS"} // Fire tablets/TVs run Android but report Fire OS to users
	}
	return res
}

var botCategoryLabels = map[BotCategory]string{
	BotSearchEngine:  "search engine",
	BotAICrawler:     "AI crawler",
	BotAIAgent:       "AI agent",
	BotSocialPreview: "link preview",
	BotMonitoring:    "monitoring",
	BotScraper:       "scraper",
	BotSEO:           "SEO",
	BotAdvertising:   "advertising",
	BotSecurity:      "security scanner",
	BotWebhook:       "webhook",
	BotFeedFetcher:   "feed fetcher",
	BotLibrary:       "HTTP library",
}

var deviceTypeLabels = map[DeviceType]string{
	DeviceDesktop:  "Desktop",
	DeviceMobile:   "Mobile",
	DeviceTablet:   "Tablet",
	DeviceTV:       "TV",
	DeviceConsole:  "Console",
	DeviceWearable: "Wearable",
	DeviceEReader:  "E-reader",
}

// String renders a human display line for session lists and audit logs:
// "Chrome 138 on macOS 14 (Desktop)", "GPTBot (AI crawler)", "Unknown".
func (ua UserAgent) String() string {
	if ua.IsBot() {
		name := ua.Bot.Name
		if name == "" {
			name = "Bot"
		}
		if label, ok := botCategoryLabels[ua.Bot.Category]; ok {
			return name + " (" + label + ")"
		}
		return name
	}
	var b strings.Builder
	if ua.Browser.Name != "" {
		b.WriteString(ua.Browser.Name)
		if ua.Browser.Version.Major > 0 {
			b.WriteByte(' ')
			b.WriteString(strconv.Itoa(ua.Browser.Version.Major))
		}
	}
	if ua.OS.Name != "" {
		if b.Len() > 0 {
			b.WriteString(" on ")
		}
		b.WriteString(ua.OS.Name)
		if v := osDisplayVersion(ua.OS.Version); v != "" {
			b.WriteByte(' ')
			b.WriteString(v)
		}
	}
	if label, ok := deviceTypeLabels[ua.Device.Type]; ok {
		if b.Len() > 0 {
			b.WriteString(" (")
			b.WriteString(label)
			b.WriteByte(')')
		} else {
			b.WriteString(label)
		}
	}
	if b.Len() == 0 {
		return "Unknown"
	}
	return b.String()
}

// osDisplayVersion shows the marketing major ("14", "10"), keeps "8.1"
// intact, and falls back to Full for non-numeric versions ("XP", "Vista").
func osDisplayVersion(v Version) string {
	if v.Full == "8.1" || v.Major == 0 {
		return v.Full
	}
	return strconv.Itoa(v.Major)
}
