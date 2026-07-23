package sse

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dmitrymomot/forge/realtime/fanout"
)

// TopicsFunc resolves the fanout topics one request subscribes to — from the
// URL, the session, the tenant, wherever the consumer keeps them. An error
// fails the request with 400 before anything is subscribed or written.
type TopicsFunc func(r *http.Request) ([]string, error)

// handler is the mountable SSE endpoint over a fanout hub.
type handler struct {
	hub        *fanout.Hub
	topics     TopicsFunc
	encode     func(fanout.Message) (Event, error)
	log        *slog.Logger
	subOpts    []fanout.SubscribeOption
	writerOpts []WriterOption
	keepAlive  time.Duration
	retry      time.Duration
}

// NewHandler builds the SSE endpoint over hub: GET only, one fanout
// subscription per request via topics, heartbeat comments while idle,
// teardown on client disconnect, and Last-Event-ID resume through the hub's
// replay ring. It returns the accumulated option errors, if any.
func NewHandler(hub *fanout.Hub, topics TopicsFunc, opts ...HandlerOption) (http.Handler, error) {
	if hub == nil {
		return nil, errors.New("sse: nil hub")
	}
	if topics == nil {
		return nil, errors.New("sse: nil topics func")
	}
	c := newHandlerConfig()
	for _, opt := range opts {
		opt(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	return &handler{
		hub:        hub,
		topics:     topics,
		encode:     c.encode,
		log:        c.log,
		subOpts:    c.subOpts,
		writerOpts: c.writerOpts,
		keepAlive:  c.keepAlive,
		retry:      c.retry,
	}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	topics, err := h.topics(r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	sub, err := h.subscribe(r, topics)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, fanout.ErrScopeMissing):
			// Fail closed: a configured tenancy hook that cannot produce a
			// scope means this request must not observe any topic.
			status = http.StatusForbidden
		case errors.Is(err, fanout.ErrClosed):
			status = http.StatusServiceUnavailable
		}
		http.Error(w, http.StatusText(status), status)
		return
	}
	defer sub.Close()
	wr, err := NewWriter(w, h.writerOpts...)
	if err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if h.retry > 0 {
		if err := wr.Send(Retry(h.retry)); err != nil {
			return
		}
	}
	var heartbeat <-chan time.Time
	if h.keepAlive > 0 {
		t := time.NewTicker(h.keepAlive)
		defer t.Stop()
		heartbeat = t.C
	}
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-sub.C():
			if !ok {
				// The hub tore the subscription down (Close or a slow
				// consumer); ending the response makes the client reconnect
				// and resume.
				return
			}
			ev, err := h.encode(msg)
			if err != nil {
				h.log.WarnContext(ctx, "sse: encode event failed, message skipped",
					slog.String("topic", msg.Topic), slog.Uint64("message_id", msg.ID), slog.Any("error", err))
				continue
			}
			if err := wr.Send(ev); err != nil {
				return
			}
		case <-heartbeat:
			if err := wr.Send(Comment("")); err != nil {
				return
			}
		}
	}
}

// subscribe opens the request's fanout subscription, resuming after the
// client's Last-Event-ID when it carries a valid cursor. A cursor the hub
// cannot honor (replay disabled) degrades to a live-only stream instead of
// failing the request.
func (h *handler) subscribe(r *http.Request, topics []string) (*fanout.Subscription, error) {
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if id, err := strconv.ParseUint(raw, 10, 64); err == nil {
			opts := make([]fanout.SubscribeOption, len(h.subOpts), len(h.subOpts)+1)
			copy(opts, h.subOpts)
			opts = append(opts, fanout.WithResumeAfter(id))
			sub, err := h.hub.Subscribe(r.Context(), topics, opts...)
			if err == nil || !errors.Is(err, fanout.ErrReplayDisabled) {
				return sub, err
			}
		}
	}
	return h.hub.Subscribe(r.Context(), topics, h.subOpts...)
}
