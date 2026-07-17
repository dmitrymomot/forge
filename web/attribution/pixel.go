package attribution

import (
	"net/http"
	"strconv"
)

// pixelGIF is a 1×1 fully transparent GIF89a — the canonical 43-byte
// tracking pixel.
var pixelGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, // header "GIF89a"
	0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, // 1×1 logical screen, 2-color global palette
	0x00, 0x00, 0x00, 0xff, 0xff, 0xff, // palette: black, white
	0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, // graphic control: color 0 transparent
	0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, // 1×1 image descriptor
	0x02, 0x02, 0x44, 0x01, 0x00, // LZW image data: one transparent pixel
	0x3b, // trailer
}

// Pixel returns the tracking-pixel endpoint: it captures campaign params
// from the pixel URL exactly like Middleware, then serves a 1×1 transparent
// GIF with caching disabled so every impression reaches the server. Embed
// it where the capture middleware can't run — emails don't carry cookies to
// this endpoint, but marketing pages served off a static host do.
func (t *Tracker) Pixel() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.capture(w, r)
		h := w.Header()
		h.Set("Content-Type", "image/gif")
		h.Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		h.Set("Pragma", "no-cache")
		h.Set("Expires", "0")
		h.Set("Content-Length", strconv.Itoa(len(pixelGIF)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(pixelGIF)
		}
	})
}
