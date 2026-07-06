package secheaders

import "strings"

// CSP source keywords and schemes.
const (
	Self          = "'self'"
	None          = "'none'"
	UnsafeInline  = "'unsafe-inline'"
	UnsafeEval    = "'unsafe-eval'"
	StrictDynamic = "'strict-dynamic'"
	Data          = "data:"
	Blob          = "blob:"
)

// Policy is a typed Content-Security-Policy. Only non-empty directives are
// emitted. When the middleware runs with WithNonce, the per-request nonce is
// appended to ScriptSrc and StyleSrc.
type Policy struct {
	ReportURI      string
	DefaultSrc     []string
	ScriptSrc      []string
	StyleSrc       []string
	ImgSrc         []string
	ConnectSrc     []string
	FontSrc        []string
	ObjectSrc      []string
	FrameAncestors []string
	BaseURI        []string
	FormAction     []string
}

// render serializes the policy; nonce is appended to script-src/style-src
// when non-empty.
func (p Policy) render(nonce string) string {
	var b strings.Builder
	dir := func(name string, srcs []string, withNonce bool) {
		if len(srcs) == 0 && (!withNonce || nonce == "") {
			return
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(name)
		for _, s := range srcs {
			b.WriteByte(' ')
			b.WriteString(s)
		}
		if withNonce && nonce != "" {
			b.WriteString(" 'nonce-")
			b.WriteString(nonce)
			b.WriteString("'")
		}
	}
	dir("default-src", p.DefaultSrc, false)
	dir("script-src", p.ScriptSrc, true)
	dir("style-src", p.StyleSrc, true)
	dir("img-src", p.ImgSrc, false)
	dir("connect-src", p.ConnectSrc, false)
	dir("font-src", p.FontSrc, false)
	dir("object-src", p.ObjectSrc, false)
	dir("frame-ancestors", p.FrameAncestors, false)
	dir("base-uri", p.BaseURI, false)
	dir("form-action", p.FormAction, false)
	if p.ReportURI != "" {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString("report-uri ")
		b.WriteString(p.ReportURI)
	}
	return b.String()
}
