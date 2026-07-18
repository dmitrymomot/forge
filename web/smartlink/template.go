package smartlink

import (
	"fmt"
	"net/url"
	"strings"
)

// macroKind identifies which visit fact a template macro renders.
type macroKind uint8

const (
	macroNone macroKind = iota // literal segment, no macro
	macroCountry
	macroDevice
	macroLocale
	macroParam
)

// escMode selects the positional escaping for a macro, decided at compile
// from the literal text preceding it: authority (between "//" and the first
// '/', '?', or '#') renders hostname-safe values verbatim and anything else
// as empty — net/url forbids percent-escapes like %40 in a host, so encoding
// isn't an option and a hostile value fails closed to a dead but well-formed
// URL; path escapes with url.PathEscape plus ':' (so a value can't smuggle a
// scheme into the first segment of a relative template); query escapes with
// url.QueryEscape. Together these keep macro values from ever altering the
// URL structure validated at compile time.
type escMode uint8

const (
	escPath escMode = iota
	escQuery
	escAuthority
)

// segment is one compiled piece of a URL template: a literal followed by an
// optional macro.
type segment struct {
	literal string
	param   string // param name for macroParam
	macro   macroKind
	esc     escMode
}

// template is a compiled URL template. A nil segs means the URL is a plain
// literal and renders with zero work. elidedURL is the one url.Parse of the
// macro-elided form (raw itself for a literal) — reused by checkTargetURL
// and the literal-target query precompute instead of re-parsing the same
// string per compile — and authMacro reports whether any macro sits in the
// authority segment; all computed here once so validation layers never
// re-scan the raw template with a second parser.
type template struct {
	elidedURL *url.URL
	raw       string
	segs      []segment
	authMacro bool
}

// parseTemplate compiles a target URL template: splits out {macro}
// placeholders, resolves the vocabulary, assigns positional escaping, and
// validates the macro-elided template parses as a URL.
func parseTemplate(raw, where string) (template, error) {
	if !strings.ContainsRune(raw, '{') && !strings.ContainsRune(raw, '}') {
		u, err := url.Parse(raw)
		if err != nil {
			return template{}, fmt.Errorf("%w: %s: %v", ErrInvalidTemplate, where, err)
		}
		return template{raw: raw, elidedURL: u}, nil
	}

	var segs []segment
	var elided strings.Builder
	var authMacro bool
	rest := raw
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(rest[open:], '}')
		if close < 0 {
			return template{}, fmt.Errorf("%w: %s: unclosed '{' in %q", ErrInvalidTemplate, where, raw)
		}
		close += open
		literal := rest[:open]
		if strings.ContainsRune(literal, '}') {
			return template{}, fmt.Errorf("%w: %s: unmatched '}' in %q", ErrInvalidTemplate, where, raw)
		}
		name := rest[open+1 : close]
		kind, param, err := resolveMacro(name, where)
		if err != nil {
			return template{}, err
		}
		elided.WriteString(literal)
		esc := escapeModeFor(elided.String())
		if esc == escAuthority {
			authMacro = true
		}
		segs = append(segs, segment{literal: literal, macro: kind, param: param, esc: esc})
		rest = rest[close+1:]
	}
	if strings.ContainsRune(rest, '}') {
		return template{}, fmt.Errorf("%w: %s: unmatched '}' in %q", ErrInvalidTemplate, where, raw)
	}
	if rest != "" {
		segs = append(segs, segment{literal: rest})
		elided.WriteString(rest)
	}
	u, err := url.Parse(elided.String())
	if err != nil {
		return template{}, fmt.Errorf("%w: %s: %v", ErrInvalidTemplate, where, err)
	}
	return template{raw: raw, elidedURL: u, segs: segs, authMacro: authMacro}, nil
}

// escapeModeFor derives a macro's escaping from the macro-elided literal
// prefix before it (macro values never create structure, so the prefix alone
// fixes the position).
func escapeModeFor(prefix string) escMode {
	if strings.ContainsRune(prefix, '?') {
		return escQuery
	}
	auth := -1
	if i := strings.Index(prefix, "://"); i >= 0 {
		auth = i + 3
	} else if strings.HasPrefix(prefix, "//") {
		auth = 2
	}
	if auth >= 0 && !strings.ContainsAny(prefix[auth:], "/?#") {
		return escAuthority
	}
	return escPath
}

// resolveMacro maps a macro name to its kind; unknown names are compile
// errors, never empty substitutions at decide time.
func resolveMacro(name, where string) (macroKind, string, error) {
	switch name {
	case "country":
		return macroCountry, "", nil
	case "device":
		return macroDevice, "", nil
	case "locale":
		return macroLocale, "", nil
	}
	if param, ok := strings.CutPrefix(name, "param."); ok && param != "" {
		return macroParam, param, nil
	}
	return macroNone, "", fmt.Errorf("%w: %s: {%s}", ErrUnknownMacro, where, name)
}

// render substitutes visit values into the template. A known macro whose
// visit value is empty renders as an empty string — the visit context is
// genuinely sparse.
func (t template) render(v *Visit) string {
	if t.segs == nil {
		return t.raw
	}
	var b strings.Builder
	b.Grow(len(t.raw) + 16)
	for _, s := range t.segs {
		b.WriteString(s.literal)
		var val string
		switch s.macro {
		case macroNone:
			continue
		case macroCountry:
			val = v.Country
		case macroDevice:
			val = v.Device
		case macroLocale:
			val = v.Locale
		case macroParam:
			val = v.Params[s.param]
		}
		if val == "" {
			continue
		}
		switch s.esc {
		case escAuthority:
			if isHostSafe(val) {
				b.WriteString(val)
			}
		case escPath:
			// ':' additionally encoded: in the first segment of a relative
			// template it would otherwise reparse as a scheme delimiter.
			b.WriteString(strings.ReplaceAll(url.PathEscape(val), ":", "%3A"))
		case escQuery:
			b.WriteString(url.QueryEscape(val))
		}
	}
	return b.String()
}

// isHostSafe reports whether every byte of s belongs to the unreserved
// hostname set, so an authority-position value can't introduce userinfo
// ('@'), a port (':'), or end the authority ('/', '?', '#').
func isHostSafe(s string) bool {
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' || c == '~' {
			continue
		}
		return false
	}
	return true
}
