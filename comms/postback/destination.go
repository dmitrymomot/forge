package postback

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// escMode selects the positional escaping for a macro, decided at parse from
// the literal text preceding it: query escapes with url.QueryEscape; path
// escapes with url.PathEscape plus ':' (so a value can't smuggle a scheme
// into the first path segment). Macros in the scheme or authority are
// rejected at NewDestination — a click ID can't pick the host it reports to.
type escMode uint8

const (
	escPath escMode = iota
	escQuery
	escAuthority
)

// segment is one parsed piece of a URL template: a literal followed by an
// optional macro.
type segment struct {
	literal string
	macro   string // "" for the literal-only tail
	esc     escMode
}

// Destination is a validated postback target: a URL template parsed against a
// Vocabulary plus the HTTP method to fire with. Construct via NewDestination;
// the zero value fails closed in Send.
type Destination struct {
	raw    string
	method string
	segs   []segment
}

// DestinationOption configures a Destination at parse time.
type DestinationOption func(*Destination)

// WithMethod sets the HTTP method Send fires with (default GET). Only GET and
// POST are accepted — macros ride the URL either way, the body stays empty.
func WithMethod(method string) DestinationOption {
	return func(d *Destination) { d.method = strings.ToUpper(method) }
}

// NewDestination parses and validates a postback URL template. Every {macro}
// must be registered in vocab (ErrUnknownMacro), the macro-elided template
// must parse as an absolute http(s) URL with no fragment, and no macro may
// sit in the authority (ErrInvalidTemplate) — all construction errors, never
// an empty substitution at fire time.
func NewDestination(raw string, vocab Vocabulary, opts ...DestinationOption) (Destination, error) {
	d := Destination{raw: raw, method: http.MethodGet}
	for _, opt := range opts {
		opt(&d)
	}
	if d.method != http.MethodGet && d.method != http.MethodPost {
		return Destination{}, fmt.Errorf("%w: %q", ErrInvalidMethod, d.method)
	}

	var elided strings.Builder
	rest := raw
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return Destination{}, fmt.Errorf("%w: unclosed '{' in %q", ErrInvalidTemplate, raw)
		}
		end += open
		literal := rest[:open]
		if strings.ContainsRune(literal, '}') {
			return Destination{}, fmt.Errorf("%w: unmatched '}' in %q", ErrInvalidTemplate, raw)
		}
		name := rest[open+1 : end]
		if !vocab.contains(name) {
			return Destination{}, fmt.Errorf("%w: {%s}", ErrUnknownMacro, name)
		}
		elided.WriteString(literal)
		esc := escapeModeFor(elided.String())
		if esc == escAuthority {
			return Destination{}, fmt.Errorf("%w: macro {%s} in scheme or authority of %q", ErrInvalidTemplate, name, raw)
		}
		d.segs = append(d.segs, segment{literal: literal, macro: name, esc: esc})
		rest = rest[end+1:]
	}
	if strings.ContainsRune(rest, '}') {
		return Destination{}, fmt.Errorf("%w: unmatched '}' in %q", ErrInvalidTemplate, raw)
	}
	if rest != "" {
		if d.segs != nil {
			d.segs = append(d.segs, segment{literal: rest})
		}
		elided.WriteString(rest)
	}

	u, err := url.Parse(elided.String())
	if err != nil {
		return Destination{}, fmt.Errorf("%w: %v", ErrInvalidTemplate, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Destination{}, fmt.Errorf("%w: scheme must be http or https in %q", ErrInvalidTemplate, raw)
	}
	if u.Host == "" {
		return Destination{}, fmt.Errorf("%w: missing host in %q", ErrInvalidTemplate, raw)
	}
	if u.Fragment != "" || strings.HasSuffix(elided.String(), "#") {
		return Destination{}, fmt.Errorf("%w: fragment never reaches the tracker in %q", ErrInvalidTemplate, raw)
	}
	return d, nil
}

// escapeModeFor derives a macro's escaping from the macro-elided literal
// prefix before it (macro values never create structure, so the prefix alone
// fixes the position). escAuthority covers everything before the first '/'
// after "://" — including a prefix with no "://" at all, i.e. a macro in or
// before the scheme: every legitimate path macro of an absolute http(s)
// template has "://" upstream, so its absence means the macro shapes the
// scheme or host and must be rejected.
func escapeModeFor(prefix string) escMode {
	if strings.ContainsRune(prefix, '?') {
		return escQuery
	}
	_, after, found := strings.Cut(prefix, "://")
	if !found || !strings.ContainsAny(after, "/?#") {
		return escAuthority
	}
	return escPath
}

// Raw returns the template as given to NewDestination.
func (d Destination) Raw() string { return d.raw }

// Method returns the HTTP method Send fires with.
func (d Destination) Method() string { return d.method }

// Render substitutes event values into the template, escaping each for its
// position. A registered macro absent from values (or empty) renders as an
// empty string — sub-IDs are genuinely sparse and trackers accept empty
// parameters.
func (d Destination) Render(values map[string]string) string {
	if d.segs == nil {
		return d.raw
	}
	var b strings.Builder
	b.Grow(len(d.raw) + 16)
	for _, s := range d.segs {
		b.WriteString(s.literal)
		if s.macro == "" {
			continue
		}
		val := values[s.macro]
		if val == "" {
			continue
		}
		switch s.esc {
		case escQuery:
			b.WriteString(url.QueryEscape(val))
		case escPath:
			switch val {
			// PathEscape leaves dots alone, so a bare dot value would render a
			// dot-segment ("/pb/../done") that path-normalizing servers collapse,
			// dropping a segment the template author fixed. Percent-encoded dots
			// are not dot-segments (RFC 3986 §3.3), so encode them.
			case ".":
				b.WriteString("%2E")
			case "..":
				b.WriteString("%2E%2E")
			default:
				// ':' additionally encoded everywhere in path position: in the
				// first segment it would otherwise reparse as a scheme delimiter,
				// and over-encoding it elsewhere is harmless.
				b.WriteString(strings.ReplaceAll(url.PathEscape(val), ":", "%3A"))
			}
		case escAuthority:
			// unreachable: rejected at NewDestination
		}
	}
	return b.String()
}
