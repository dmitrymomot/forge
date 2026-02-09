package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type sseEventKind int

const (
	kindString sseEventKind = iota
	kindJSON
	kindHTML
	kindComment
	kindRetry
)

const defaultSSEKeepAlive = 30 * time.Second

type SSEEvent struct {
	jsonData  any
	component Component
	event     string
	data      string
	kind      sseEventKind
	retryMs   int
}

func SSEString(event, data string) SSEEvent {
	return SSEEvent{
		kind:  kindString,
		event: event,
		data:  data,
	}
}

func SSEJSON(event string, v any) SSEEvent {
	return SSEEvent{
		kind:     kindJSON,
		event:    event,
		jsonData: v,
	}
}

func SSETempl(event string, component Component) SSEEvent {
	return SSEEvent{
		kind:      kindHTML,
		event:     event,
		component: component,
	}
}

func SSEComment(text string) SSEEvent {
	return SSEEvent{
		kind: kindComment,
		data: text,
	}
}

func SSERetry(ms int) SSEEvent {
	return SSEEvent{
		kind:    kindRetry,
		retryMs: ms,
	}
}

func initSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func writeSSEEvent(w *ResponseWriter, ctx context.Context, evt SSEEvent) error {
	switch evt.kind {
	case kindString:
		if evt.event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", evt.event); err != nil {
				return nil
			}
		}
		lines := strings.Split(evt.data, "\n")
		for _, line := range lines {
			if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
				return nil
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return nil
		}

	case kindJSON:
		marshaled, err := json.Marshal(evt.jsonData)
		if err != nil {
			return err
		}
		if evt.event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", evt.event); err != nil {
				return nil
			}
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", marshaled); err != nil {
			return nil
		}

	case kindHTML:
		var buf bytes.Buffer
		if err := evt.component.Render(ctx, &buf); err != nil {
			return err
		}
		if evt.event != "" {
			if _, err := fmt.Fprintf(w, "event: %s\n", evt.event); err != nil {
				return nil
			}
		}
		lines := strings.Split(buf.String(), "\n")
		for _, line := range lines {
			if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
				return nil
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return nil
		}

	case kindComment:
		if _, err := fmt.Fprintf(w, ": %s\n\n", evt.data); err != nil {
			return nil
		}

	case kindRetry:
		if _, err := fmt.Fprintf(w, "retry: %d\n\n", evt.retryMs); err != nil {
			return nil
		}
	}

	w.Flush()
	return nil
}
