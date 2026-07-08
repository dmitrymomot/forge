package useragent

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
	res.Browser = detectBrowser(in)
	res.OS = detectOS(in)
	res.Device = detectDevice(in, res.OS.Name)
	if res.Device.Brand == "Amazon" && res.OS.Name == "Android" {
		res.OS = OS{Name: "Fire OS"} // Fire devices run Android but report Fire OS to users
	}
	return res
}
