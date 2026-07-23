package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Event is one Server-Sent Events frame. The zero value frames to a blank
// line, which every client ignores; set fields directly or use the Text,
// JSON, Comment, and Retry constructors. Multi-line Data is framed as
// multiple "data:" lines and reassembles into the same string client-side.
type Event struct {
	// ID sets the "id:" field: the client's Last-Event-ID resume cursor. It
	// must not contain line breaks or NUL.
	ID string
	// Name sets the "event:" field. Empty means the client's default
	// "message" event; EventSource dispatches any other name only to
	// addEventListener(name) listeners, not onmessage. It must not contain
	// line breaks.
	Name string

	comment string
	// Data is the event payload, framed as one "data:" line per line of
	// input. A nil Data frames no "data:" line at all, which makes the
	// client ignore the frame (an ID-only frame still advances the resume
	// cursor); an empty non-nil Data dispatches an empty string.
	Data []byte
	// Retry sets the "retry:" field: the client's reconnection delay,
	// rounded up to a whole millisecond. Zero omits the field; negative is
	// invalid.
	Retry time.Duration

	hasComment bool
}

// Text builds a named event carrying a plain-text payload. An empty name
// means the client's default "message" event.
func Text(name, data string) Event {
	return Event{Name: name, Data: []byte(data)}
}

// JSON builds a named event carrying the JSON encoding of v. An empty name
// means the client's default "message" event.
func JSON(name string, v any) (Event, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Event{}, fmt.Errorf("sse: marshal event data: %w", err)
	}
	return Event{Name: name, Data: data}, nil
}

// Component is the structural seam for templ components — anything with
// templ's Render method satisfies it implicitly, so sse never imports templ
// and html/template users can adapt with a small func type.
type Component interface {
	Render(ctx context.Context, w io.Writer) error
}

// Templ builds a named event carrying the rendered HTML of c. Multi-line
// output is framed as multiple "data:" lines and reassembles byte-identical
// client-side (CR/CRLF normalize to LF), so htmx's sse extension swaps it in
// unchanged. An empty name means the client's default "message" event.
func Templ(ctx context.Context, name string, c Component) (Event, error) {
	if c == nil {
		return Event{}, fmt.Errorf("sse: nil component")
	}
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		return Event{}, fmt.Errorf("sse: render component: %w", err)
	}
	data := buf.Bytes()
	if data == nil {
		// A component that rendered nothing still dispatches an empty event
		// (matching Text(name, "")); nil Data would drop the frame entirely.
		data = []byte{}
	}
	return Event{Name: name, Data: data}, nil
}

// Comment builds a comment-only frame (": text"). Clients ignore comments;
// they exist to keep idle connections alive through proxies. Multi-line text
// frames as one comment line per line of input.
func Comment(text string) Event {
	return Event{comment: text, hasComment: true}
}

// Retry builds a frame carrying only a reconnection-delay advice for the
// client, rounded up to a whole millisecond.
func Retry(d time.Duration) Event {
	return Event{Retry: d}
}

// appendTo validates e and appends its wire frame to b.
func (e Event) appendTo(b []byte) ([]byte, error) {
	if strings.ContainsAny(e.Name, "\r\n") {
		return nil, fmt.Errorf("%w: event name contains a line break", ErrInvalidEvent)
	}
	if strings.ContainsAny(e.ID, "\r\n\x00") {
		return nil, fmt.Errorf("%w: id contains a line break or NUL", ErrInvalidEvent)
	}
	if e.Retry < 0 {
		return nil, fmt.Errorf("%w: negative retry %s", ErrInvalidEvent, e.Retry)
	}
	if e.hasComment {
		b = appendLines(b, ": ", []byte(e.comment))
	}
	if e.ID != "" {
		b = append(b, "id: "...)
		b = append(b, e.ID...)
		b = append(b, '\n')
	}
	if e.Name != "" {
		b = append(b, "event: "...)
		b = append(b, e.Name...)
		b = append(b, '\n')
	}
	if e.Retry > 0 {
		b = append(b, "retry: "...)
		ms := int64((e.Retry + time.Millisecond - 1) / time.Millisecond)
		b = strconv.AppendInt(b, ms, 10)
		b = append(b, '\n')
	}
	if e.Data != nil {
		b = appendLines(b, "data: ", e.Data)
	}
	return append(b, '\n'), nil
}

// appendLines appends value as one prefixed field line per line of input,
// treating \n, \r, and \r\n all as line breaks (raw line breaks are illegal
// inside a field line).
func appendLines(b []byte, prefix string, value []byte) []byte {
	b = append(b, prefix...)
	for i := 0; i < len(value); i++ {
		switch c := value[i]; c {
		case '\n':
			b = append(b, '\n')
			b = append(b, prefix...)
		case '\r':
			if i+1 < len(value) && value[i+1] == '\n' {
				i++
			}
			b = append(b, '\n')
			b = append(b, prefix...)
		default:
			b = append(b, c)
		}
	}
	return append(b, '\n')
}
