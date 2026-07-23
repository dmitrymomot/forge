package sse_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/sse"
)

func Example() {
	ctx := context.Background()

	hub, err := fanout.New(fanout.WithReplay(256))
	if err != nil {
		panic(err)
	}
	defer hub.Close()

	handler, err := sse.NewHandler(hub, func(r *http.Request) ([]string, error) {
		return []string{"notifications.42"}, nil
	})
	if err != nil {
		panic(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// The browser side of this is: new EventSource("/events") plus
	// addEventListener("notifications.42", ...).
	resp, err := http.Get(srv.URL)
	if err != nil || resp == nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := hub.Publish(ctx, "notifications.42", []byte(`{"unread":3}`)); err != nil {
		panic(err)
	}

	br := bufio.NewReader(resp.Body)
	// The frame opens with an "id:" line — an opaque, per-instance resume cursor
	// — which we skip here, followed by the event and data lines.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			panic(err)
		}
		if strings.HasPrefix(line, "id: ") {
			continue
		}
		fmt.Print(line)
		if strings.HasPrefix(line, "data: ") {
			break
		}
	}

	// Output:
	// event: notifications.42
	// data: {"unread":3}
}

func ExampleNewWriter() {
	rec := httptest.NewRecorder() // any flushable http.ResponseWriter

	// A recorder cannot set write deadlines, so WithSendTimeout(0) opts out of
	// the per-send bound; a real server connection supports it, so production
	// code keeps the default.
	w, err := sse.NewWriter(rec, sse.WithSendTimeout(0))
	if err != nil {
		panic(err) // ErrStreamingUnsupported: respond 500 instead
	}
	if err := w.Send(sse.Text("progress", "building")); err != nil {
		panic(err)
	}
	ev, err := sse.JSON("progress", map[string]int{"done": 80})
	if err != nil {
		panic(err)
	}
	if err := w.Send(ev); err != nil {
		panic(err)
	}

	fmt.Print(rec.Body.String())
	// Output:
	// event: progress
	// data: building
	//
	// event: progress
	// data: {"done":80}
}
