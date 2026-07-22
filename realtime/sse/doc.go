// Package sse serves Server-Sent Events: the mountable live-updates endpoint
// over a fanout hub, and the low-level event writer it is built on. Delivery
// is the realtime contract — ephemeral, at-most-once, to currently-connected
// clients; durable messaging is async/eventbus.
//
// # Endpoint over fanout
//
// NewHandler turns a hub into an endpoint: each GET request subscribes to the
// topics your TopicsFunc resolves for it, streams every published message as
// an SSE event (topic as the event name, hub message ID as the "id:" resume
// cursor), sends heartbeat comments while idle, and tears the subscription
// down when the client disconnects.
//
//	hub, _ := fanout.New(fanout.WithReplay(256))
//	defer hub.Close()
//
//	handler, _ := sse.NewHandler(hub, func(r *http.Request) ([]string, error) {
//		return []string{"notifications." + userID(r)}, nil
//	})
//	mux.Handle("GET /events", handler)
//
//	// elsewhere:
//	_ = hub.Publish(ctx, "notifications.42", []byte(`{"unread":3}`))
//
// With replay enabled on the hub, a reconnecting EventSource automatically
// resumes: the browser sends Last-Event-ID and the handler replays the missed
// messages from the ring before going live. Hub message IDs are per-instance
// — after a restart or behind a round-robin balancer a stale cursor degrades
// to a live-only stream, never an error. Multi-tenant apps configure
// fanout.WithScope on the hub; the handler passes the request context
// through, so topics stay tenant-isolated with zero ceremony here, and a
// missing scope fails the request closed with 403.
//
// The default event name is the topic: EventSource dispatches those only to
// addEventListener(topic) listeners (htmx's sse extension keys sse-swap the
// same way). For plain onmessage clients, install a WithEncoder that clears
// Event.Name.
//
// # Low-level writer
//
// Writer is the brick under the handler and under bridges like web/htmx and
// llm: it sets the stream headers, then frames and flushes one event per
// Send.
//
//	w, err := sse.NewWriter(rw) // rw is the http.ResponseWriter
//	if err != nil { ... }       // ErrStreamingUnsupported: respond 500
//	_ = w.Send(sse.Text("update", "building"))
//	ev, _ := sse.JSON("update", payload)
//	_ = w.Send(ev)
//
// # Deployment
//
// An SSE response never finishes, so the server's write timeout must not
// apply to it. NewWriter clears the write deadline through
// http.ResponseController, which works on net/http servers as long as every
// wrapping middleware implements Unwrap (all forge middleware does). If a
// deadline-oblivious wrapper sits in the chain, run the server with
// httpserver WriteTimeout=0 instead. In place of the cleared deadline, every
// Send arms a per-write one (WithSendTimeout, default 30s), so a client that
// stays connected but stops reading fails the stream instead of pinning the
// connection forever. Heartbeats (WithKeepAlive, default 15s) keep proxies
// from cutting idle streams — and give a silent topic regular Sends, which is
// what makes the send timeout catch stalled clients even with no traffic —
// and the X-Accel-Buffering header disables nginx response buffering.
package sse
