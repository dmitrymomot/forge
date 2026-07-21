package fanout

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
)

// scopeSep joins a tenant scope and a topic into one registry key. It is
// reserved: topics and scopes containing it (or NUL) are rejected.
const scopeSep = '\x1f'

// topicState is one topic's registry entry: its live subscribers and, with
// replay enabled, the ring of its newest messages.
type topicState struct {
	lastPub time.Time
	subs    map[*Subscription]struct{}
	ring    *ring
	topic   string
	mu      sync.Mutex
}

// Hub is the in-process pub/sub hub. Construct with New; all methods are safe
// for concurrent use.
type Hub struct {
	clk       clock.Clock
	bus       Bus
	scope     func(ctx context.Context) (string, error)
	topics    map[string]*topicState
	replayTTL time.Duration
	buffer    int
	replay    int
	nextID    atomic.Uint64
	lastSweep atomic.Int64
	mu        sync.RWMutex
	// closed is written under mu but read lock-free on the publish hot path.
	closed atomic.Bool
	policy OverflowPolicy
}

// New builds a Hub. It returns the accumulated option errors, if any, before
// anything else. With WithBus configured it also registers the hub as the
// bus's delivery target.
func New(opts ...Option) (*Hub, error) {
	c := &config{
		clk:       clock.System(),
		buffer:    defaultBuffer,
		replayTTL: defaultReplayTTL,
		policy:    DropOldest,
	}
	for _, opt := range opts {
		opt(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	h := &Hub{
		clk:       c.clk,
		bus:       c.bus,
		scope:     c.scope,
		topics:    make(map[string]*topicState),
		replayTTL: c.replayTTL,
		buffer:    c.buffer,
		replay:    c.replay,
		policy:    c.policy,
	}
	h.lastSweep.Store(h.clk.Now().UnixNano())
	if h.bus != nil {
		h.bus.Subscribe(h.dispatch)
	}
	return h, nil
}

// Publish delivers payload to every current subscriber of topic,
// at-most-once, never blocking: a full subscriber buffer takes that
// subscription's overflow policy instead. Publishing to a topic with no
// subscribers delivers nothing and is not an error. With a Bus configured
// the message routes through the bus and local delivery happens on the bus
// receive path; the returned error is then the bus publish error.
func (h *Hub) Publish(ctx context.Context, topic string, payload []byte) error {
	// Closed wins over every other error so a shut-down hub reports ErrClosed
	// even when the scope hook would also fail.
	if h.closed.Load() {
		return ErrClosed
	}
	if err := validateTopic(topic); err != nil {
		return err
	}
	key, err := h.key(ctx, topic)
	if err != nil {
		return err
	}
	if h.bus != nil {
		return h.bus.Publish(ctx, key, payload)
	}
	h.dispatch(key, payload)
	return nil
}

// Subscribe registers a subscriber on one or more topics (duplicates are
// collapsed) and returns its delivery stream. The context is used only to
// resolve the tenant scope; the subscription lives until Close — callers own
// its lifecycle:
//
//	sub, err := hub.Subscribe(ctx, []string{"chat.42"})
//	if err != nil { ... }
//	defer sub.Close()
func (h *Hub) Subscribe(ctx context.Context, topics []string, opts ...SubscribeOption) (*Subscription, error) {
	if len(topics) == 0 {
		return nil, ErrNoTopics
	}
	sc := &subConfig{buffer: h.buffer, policy: h.policy}
	for _, opt := range opts {
		opt(sc)
	}
	if len(sc.errs) > 0 {
		return nil, errors.Join(sc.errs...)
	}
	if sc.resume && h.replay == 0 {
		return nil, ErrReplayDisabled
	}
	for _, t := range topics {
		if err := validateTopic(t); err != nil {
			return nil, err
		}
	}
	keyed := make([]struct{ key, topic string }, 0, len(topics))
	for _, t := range topics {
		key, err := h.key(ctx, t)
		if err != nil {
			return nil, err
		}
		keyed = append(keyed, struct{ key, topic string }{key, t})
	}
	// Sorted unique keys give a stable multi-topic lock order.
	slices.SortFunc(keyed, func(a, b struct{ key, topic string }) int { return strings.Compare(a.key, b.key) })
	keyed = slices.CompactFunc(keyed, func(a, b struct{ key, topic string }) bool { return a.key == b.key })

	sub := &Subscription{
		hub:    h,
		ch:     make(chan Message, sc.buffer),
		policy: sc.policy,
	}
	h.mu.Lock()
	if h.closed.Load() {
		h.mu.Unlock()
		return nil, ErrClosed
	}
	states := make([]*topicState, 0, len(keyed))
	keys := make([]string, 0, len(keyed))
	for _, kt := range keyed {
		ts := h.topics[kt.key]
		if ts == nil {
			ts = h.newTopicState(kt.topic)
			h.topics[kt.key] = ts
		}
		states = append(states, ts)
		keys = append(keys, kt.key)
	}
	// Assign before registering: a publisher already waiting on a topic lock
	// can tear the subscription down (CloseSlow) the moment it is visible,
	// and that path reads states/keys.
	sub.states = states
	sub.keys = keys
	for _, ts := range states {
		ts.mu.Lock()
	}
	if sc.resume {
		h.preload(sub, states, sc.resumeAfter)
	}
	for _, ts := range states {
		ts.subs[sub] = struct{}{}
		ts.mu.Unlock()
	}
	h.mu.Unlock()
	return sub, nil
}

// Close shuts the hub down: every open subscription is torn down with
// ErrClosed and further Publish/Subscribe calls fail with ErrClosed.
// Idempotent.
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed.Load() {
		h.mu.Unlock()
		return
	}
	h.closed.Store(true)
	topics := h.topics
	h.topics = make(map[string]*topicState)
	h.mu.Unlock()

	seen := make(map[*Subscription]struct{})
	for _, ts := range topics {
		ts.mu.Lock()
		for sub := range ts.subs {
			seen[sub] = struct{}{}
		}
		clear(ts.subs)
		ts.mu.Unlock()
	}
	for sub := range seen {
		sub.closeWith(ErrClosed)
	}
}

// dispatch delivers one message locally. It is both the direct Publish path
// (no bus) and the bus receive callback.
func (h *Hub) dispatch(key string, payload []byte) {
	ts := h.lookup(key)
	if ts == nil {
		return
	}
	var stale []*Subscription
	ts.mu.Lock()
	msg := Message{Topic: ts.topic, Payload: payload, ID: h.nextID.Add(1)}
	if ts.ring != nil {
		ts.ring.push(msg)
		ts.lastPub = h.clk.Now()
	}
	for sub := range ts.subs {
		if sub.send(msg) {
			delete(ts.subs, sub)
			stale = append(stale, sub)
		}
	}
	empty := len(ts.subs) == 0 && ts.ring == nil
	ts.mu.Unlock()
	for _, sub := range stale {
		h.removeSub(sub)
	}
	if empty {
		h.gcTopic(key, ts)
	}
	if h.replay > 0 {
		h.maybeSweep()
	}
}

// lookup returns the topic's registry entry, creating one when replay is
// enabled so late resubscribers can resume from the ring.
func (h *Hub) lookup(key string) *topicState {
	h.mu.RLock()
	ts := h.topics[key]
	h.mu.RUnlock()
	if ts != nil || h.replay == 0 {
		return ts
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed.Load() {
		return nil
	}
	if ts = h.topics[key]; ts != nil {
		return ts
	}
	ts = h.newTopicState(h.display(key))
	h.topics[key] = ts
	return ts
}

// newTopicState builds a registry entry for the given display topic. Callers
// hold h.mu.
func (h *Hub) newTopicState(topic string) *topicState {
	ts := &topicState{
		subs:    make(map[*Subscription]struct{}),
		topic:   topic,
		lastPub: h.clk.Now(),
	}
	if h.replay > 0 {
		ts.ring = newRing(h.replay)
	}
	return ts
}

// preload merges the subscribed rings' backlog newer than after into the
// subscription buffer, newest-window if it overflows. Callers hold h.mu and
// every state's mutex, so no publish can slip between replay and going live.
func (h *Hub) preload(sub *Subscription, states []*topicState, after uint64) {
	var backlog []Message
	for _, ts := range states {
		if ts.ring != nil {
			backlog = ts.ring.since(after, backlog)
		}
	}
	if len(backlog) == 0 {
		return
	}
	slices.SortFunc(backlog, func(a, b Message) int { return cmp.Compare(a.ID, b.ID) })
	if over := len(backlog) - cap(sub.ch); over > 0 {
		backlog = backlog[over:]
		sub.dropped.Add(uint64(over))
	}
	for _, m := range backlog {
		sub.ch <- m
	}
}

// removeSub detaches a closed subscription from every topic it was
// registered on and garbage-collects topics left with no subscribers and no
// ring.
func (h *Hub) removeSub(sub *Subscription) {
	for i, ts := range sub.states {
		ts.mu.Lock()
		delete(ts.subs, sub)
		empty := len(ts.subs) == 0 && ts.ring == nil
		ts.mu.Unlock()
		if empty {
			h.gcTopic(sub.keys[i], ts)
		}
	}
}

// gcTopic drops the topic's registry entry if it is still the registered one
// and still empty.
func (h *Hub) gcTopic(key string, ts *topicState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.topics[key] != ts {
		return
	}
	ts.mu.Lock()
	empty := len(ts.subs) == 0 && ts.ring == nil
	ts.mu.Unlock()
	if empty {
		delete(h.topics, key)
	}
}

// maybeSweep evicts replay rings of subscriber-less topics idle past the
// replay TTL. Amortized: it runs at most once per TTL, from the publish
// path.
func (h *Hub) maybeSweep() {
	now := h.clk.Now().UnixNano()
	last := h.lastSweep.Load()
	if now-last < int64(h.replayTTL) {
		return
	}
	if !h.lastSweep.CompareAndSwap(last, now) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, ts := range h.topics {
		ts.mu.Lock()
		expired := len(ts.subs) == 0 && now-ts.lastPub.UnixNano() >= int64(h.replayTTL)
		ts.mu.Unlock()
		if expired {
			delete(h.topics, key)
		}
	}
}

// key namespaces topic with the tenant scope when configured; fail-closed on
// a missing or invalid scope.
func (h *Hub) key(ctx context.Context, topic string) (string, error) {
	if h.scope == nil {
		return topic, nil
	}
	scope, err := h.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScopeMissing, err)
	}
	if scope == "" {
		return "", ErrScopeMissing
	}
	if strings.IndexByte(scope, 0) >= 0 || strings.IndexByte(scope, scopeSep) >= 0 {
		return "", ErrInvalidScope
	}
	return scope + "\x1f" + topic, nil
}

// display strips the scope prefix off a registry key, recovering the topic
// the subscriber sees.
func (h *Hub) display(key string) string {
	if h.scope == nil {
		return key
	}
	if _, topic, ok := strings.Cut(key, "\x1f"); ok {
		return topic
	}
	return key
}

func validateTopic(topic string) error {
	if topic == "" {
		return fmt.Errorf("%w: empty", ErrInvalidTopic)
	}
	if strings.IndexByte(topic, 0) >= 0 || strings.IndexByte(topic, scopeSep) >= 0 {
		return fmt.Errorf("%w: %q contains a reserved byte", ErrInvalidTopic, topic)
	}
	return nil
}
