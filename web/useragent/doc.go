// Package useragent parses User-Agent strings and UA Client Hints headers
// into structured browser, OS, device, and bot facts. It targets session
// device lists and audit-log display lines: canonical names, marketing OS
// versions (Windows NT 6.3 → "8.1"), device type/brand/model, and a
// categorized bot taxonomy (search engines, AI crawlers, link previews,
// monitors, HTTP libraries, ...).
//
// Parsing never fails: unrecognized input yields zero values with
// Device.Type == DeviceUnknown and Raw preserved. Modern Chromium browsers
// freeze the UA string (capped version, Windows always "10", macOS always
// "10.15.7", Brave/Arc invisible), so prefer ParseRequest/ParseHeaders at
// the HTTP boundary — they overlay real values from Sec-CH-UA-* headers
// when present.
//
// Bot detection layers a hand-curated table (accurate name, category,
// operator) over a table generated from the crawler-user-agents dataset
// (go:generate, pinned commit — see gen/) plus generic heuristics ("bot"
// suffix, "+http" comment URLs). Curated attribution wins on conflict.
//
// What this is NOT: it does not detect CPU architecture or screen metrics,
// it cannot see Apple hardware beyond "iPhone"/"iPad" (Safari never exposes
// it), it cannot distinguish desktop-mode iPad Safari from a real Mac
// (that requires client-side JS), and it does not fetch remote bot lists at
// runtime — dataset updates are reviewed code changes.
//
// # Usage
//
//	ua := useragent.ParseRequest(r) // Client Hints-aware
//	if ua.IsBot() {
//		log.Info("crawler", "bot", ua.Bot.Name, "category", ua.Bot.Category)
//		return
//	}
//	session.Device = ua.String() // "Chrome 138 on macOS 14 (Desktop)"
//
// For raw strings from storage (session rows, log pipelines) use Parse:
//
//	ua := useragent.Parse(storedUA)
package useragent
