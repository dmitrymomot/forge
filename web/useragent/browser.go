package useragent

// browserRule maps a distinctive lowercase token to a canonical browser
// name. vtok names the token whose trailing characters carry the version;
// empty vtok means the identifying token itself.
type browserRule struct {
	token string
	name  string
	vtok  string
}

// Order matters: Chromium forks and in-app browsers embed "Chrome/", and
// nearly everything embeds "Safari/", so distinctive tokens come first,
// Chrome second-to-last, Safari last.
var browserRules = []browserRule{
	{token: "edg/", name: "Edge"},
	{token: "edga/", name: "Edge"},
	{token: "edgios/", name: "Edge"},
	{token: "edge/", name: "Edge"},
	{token: "opr/", name: "Opera"},
	{token: "opios/", name: "Opera"},
	{token: "opera mini/", name: "Opera Mini"},
	{token: "opera/", name: "Opera", vtok: "version/"},
	{token: "samsungbrowser/", name: "Samsung Internet"},
	{token: "vivaldi/", name: "Vivaldi"},
	{token: "yabrowser/", name: "Yandex"},
	{token: "ucbrowser/", name: "UC Browser"},
	{token: "micromessenger/", name: "WeChat"},
	{token: "mqqbrowser/", name: "QQ Browser"},
	{token: "qqbrowser/", name: "QQ Browser"},
	{token: "duckduckgo/", name: "DuckDuckGo"},
	{token: "ddg/", name: "DuckDuckGo"},
	{token: "whale/", name: "Whale"},
	{token: "miuibrowser/", name: "MIUI Browser"},
	{token: "huaweibrowser/", name: "Huawei Browser"},
	{token: "fbav/", name: "Facebook"},
	{token: "fban", name: "Facebook", vtok: "fbav/"},
	{token: "instagram ", name: "Instagram"},
	{token: "musical_ly", name: "TikTok", vtok: "app_version/"},
	{token: "bytedancewebview", name: "TikTok", vtok: "app_version/"},
	{token: " line/", name: "Line"},
	{token: "gsa/", name: "Google App"},
	{token: "fxios/", name: "Firefox"},
	{token: "firefox/", name: "Firefox"},
	{token: "crios/", name: "Chrome"},
	{token: "; wv)", name: "Android WebView", vtok: "chrome/"},
	{token: "chrome/", name: "Chrome"},
	{token: "safari/", name: "Safari", vtok: "version/"},
}

func detectBrowser(in input) Browser {
	for _, r := range browserRules {
		if !in.contains(r.token) {
			continue
		}
		vtok := r.vtok
		if vtok == "" {
			vtok = r.token
		}
		b := Browser{Name: r.name, Version: in.versionAfter(vtok)}
		if r.name == "Safari" {
			if b.Version.IsZero() {
				continue // bare "Safari/537.36" WebKit product token, not the Safari browser
			}
			if in.contains("iphone") || in.contains("ipad") || in.contains("ipod") {
				b.Name = "Mobile Safari"
			}
		}
		return b
	}
	// WebKit view on an iOS device with no Safari Version/ token → in-app webview.
	if in.contains("applewebkit/") && (in.contains("iphone") || in.contains("ipad")) {
		return Browser{Name: "iOS WebView"}
	}
	return Browser{}
}
