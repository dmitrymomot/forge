package useragent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/useragent"
)

func TestParseDevices(t *testing.T) {
	tests := []struct {
		name  string
		ua    string
		typ   useragent.DeviceType
		brand string
		model string
	}{
		{"windows desktop", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36", useragent.DeviceDesktop, "", ""},
		{"mac desktop", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15", useragent.DeviceDesktop, "", ""},
		{"linux desktop", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0", useragent.DeviceDesktop, "", ""},
		{"iphone", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1", useragent.DeviceMobile, "Apple", "iPhone"},
		{"ipad", "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1", useragent.DeviceTablet, "Apple", "iPad"},
		{"samsung phone with build", "Mozilla/5.0 (Linux; Android 14; SM-S918B Build/UP1A.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "Samsung", "SM-S918B"},
		{"samsung tablet no mobile token", "Mozilla/5.0 (Linux; Android 13; SM-X710) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36", useragent.DeviceTablet, "Samsung", "SM-X710"},
		{"pixel", "Mozilla/5.0 (Linux; Android 15; Pixel 8 Build/AP4A.250105.002) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "Google", "Pixel 8"},
		{"xiaomi redmi", "Mozilla/5.0 (Linux; Android 13; Redmi Note 12 Build/TKQ1.221114.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.5993.80 Mobile Safari/537.36", useragent.DeviceMobile, "Xiaomi", "Redmi Note 12"},
		{"oppo cph", "Mozilla/5.0 (Linux; Android 14; CPH2451 Build/UKQ1.230924.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "OPPO", "CPH2451"},
		{"realme rmx", "Mozilla/5.0 (Linux; U; Android 12; en-US; RMX3085 Build/SP1A.210812.016) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.4896.58 UCBrowser/13.4.0.1306 Mobile Safari/537.36", useragent.DeviceMobile, "realme", "RMX3085"},
		{"reduced ua frozen model K", "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "", ""},
		{"legacy locale segment is not a model", "Mozilla/5.0 (Linux; U; Android 2.3.4; en-us; Sprint APA7373KT) AppleWebKit/533.1 (KHTML, like Gecko) Version/4.0 Mobile Safari/533.1", useragent.DeviceMobile, "", ""},
		{"opera mini is mobile", "Opera/9.80 (Android; Opera Mini/12.0.1987/37.7327; U; en) Presto/2.12.423 Version/12.16", useragent.DeviceMobile, "", ""},
		{"windows phone lumia", "Mozilla/5.0 (Windows Phone 8.1; ARM; Trident/7.0; Touch; rv:11.0; IEMobile/11.0; NOKIA; Lumia 640) like Gecko", useragent.DeviceMobile, "Nokia", ""},
		{"fire tablet", "Mozilla/5.0 (Linux; Android 9; KFMAWI Build/PS7312.3038N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36", useragent.DeviceTablet, "Amazon", "KFMAWI"},
		{"fire tv", "Mozilla/5.0 (Linux; Android 9; AFTKA Build/PS7285.2877N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36", useragent.DeviceTV, "Amazon", "AFTKA"},
		{"android tv bravia", "Mozilla/5.0 (Linux; Android 12; BRAVIA 4K VH2 Build/SOF1.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36", useragent.DeviceTV, "Sony", "BRAVIA 4K VH2"},
		{"tizen smart tv", "Mozilla/5.0 (SMART-TV; Linux; Tizen 6.0) AppleWebKit/537.36 (KHTML, like Gecko) 76.0.3809.146/6.0 TV Safari/537.36", useragent.DeviceTV, "", ""},
		{"webos tv", "Mozilla/5.0 (Web0S; Linux/SmartTV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.88 Safari/537.36 WebAppManager", useragent.DeviceTV, "", ""},
		{"chromecast", "Mozilla/5.0 (X11; Linux aarch64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 CrKey/1.56.500000", useragent.DeviceTV, "Google", "Chromecast"},
		{"roku", "Roku/DVP-12.0 (12.0.0.4182-88)", useragent.DeviceTV, "Roku", ""},
		{"apple tv", "AppleTV11,1/11.1", useragent.DeviceTV, "Apple", "Apple TV"},
		{"playstation 5", "Mozilla/5.0 (PlayStation; PlayStation 5/2.26) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0 Safari/605.1.15", useragent.DeviceConsole, "Sony", "PlayStation 5"},
		{"xbox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; Xbox; Xbox One) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/48.0.2564.82 Safari/537.36 Edge/20.02", useragent.DeviceConsole, "Microsoft", "Xbox"},
		{"nintendo switch", "Mozilla/5.0 (Nintendo Switch; WifiWebAuthApplet) AppleWebKit/609.4 (KHTML, like Gecko) NF/6.0.2.22.4 NintendoBrowser/5.1.0.22433", useragent.DeviceConsole, "Nintendo", "Switch"},
		{"apple watch", "Mozilla/5.0 (Apple Watch; CPU WatchOS 10_5 like Mac OS X) AppleWebKit/605.1.15", useragent.DeviceWearable, "Apple", "Apple Watch"},
		{"galaxy watch", "Mozilla/5.0 (Linux; Android 13; Galaxy Watch6 Build/TWQ1.230512.001) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Mobile Safari/537.36", useragent.DeviceWearable, "Samsung", "Galaxy Watch6"},
		{"kindle ereader", "Mozilla/5.0 (X11; U; Linux armv7l like Android; en-us) AppleWebKit/531.2+ (KHTML, like Gecko) Version/5.0 Safari/533.2+ Kindle/3.0+", useragent.DeviceEReader, "Amazon", "Kindle"},
		{"kobo ereader", "Mozilla/5.0 (Linux; U; Android 2.0; en-us;) AppleWebKit/538.1 (KHTML, like Gecko) Kobo Touch/4.38.23171", useragent.DeviceEReader, "Kobo", ""},
		{"cubot phone is not a bot", "Mozilla/5.0 (Linux; Android 13; Cubot KingKong 9 Build/TP1A.220624.014) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Mobile Safari/537.36", useragent.DeviceMobile, "", "Cubot KingKong 9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := useragent.Parse(tt.ua)
			assert.Equal(t, tt.typ, got.Device.Type, "type")
			assert.Equal(t, tt.brand, got.Device.Brand, "brand")
			assert.Equal(t, tt.model, got.Device.Model, "model")
		})
	}
}

func TestFireDeviceReportsFireOS(t *testing.T) {
	got := useragent.Parse("Mozilla/5.0 (Linux; Android 9; KFMAWI Build/PS7312.3038N) AppleWebKit/537.36 (KHTML, like Gecko) Silk/126.2.7 like Chrome/126.0.6478.71 Safari/537.36")
	assert.Equal(t, "Fire OS", got.OS.Name)
}
