package sse_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

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
	for range 3 { // id line, event line, data line
		line, err := br.ReadString('\n')
		if err != nil {
			panic(err)
		}
		fmt.Print(line)
	}

	// Output:
	// id: 1
	// event: notifications.42
	// data: {"unread":3}
}

func ExampleNewWriter() {
	rec := httptest.NewRecorder() // any flushable http.ResponseWriter

	w, err := sse.NewWriter(rec)
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
