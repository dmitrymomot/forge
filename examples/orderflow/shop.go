package main

import (
	"fmt"
	"sync"

	"github.com/dmitrymomot/forge/async/eventbus"
)

// The two order lifecycle events. order.placed enters the pipeline through
// the outbox; order.completed is published by the fulfillment workflow.
var (
	evtOrderPlaced    = eventbus.NewEvent[orderEvent]("order.placed")
	evtOrderCompleted = eventbus.NewEvent[orderEvent]("order.completed")
)

// orderEvent is the payload both lifecycle events carry.
type orderEvent struct {
	OrderID string `json:"order_id"`
	Item    string `json:"item"`
	Qty     int    `json:"qty"`
}

// shop is the "business database" stand-in: stock levels and order statuses
// behind a mutex. In production these are rows the outbox transaction spans.
type shop struct {
	stock    map[string]int
	reserved map[string]bool   // order id -> stock already taken for it
	orders   map[string]string // order id -> placed | fulfilled | failed
	mu       sync.Mutex
	seq      int
}

func newShop() *shop {
	return &shop{
		stock:    map[string]int{"espresso": 100, "unicorn": 0},
		reserved: make(map[string]bool),
		orders:   make(map[string]string),
	}
}

// place records a new order and returns its id.
func (s *shop) place() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("ord-%d", s.seq)
	s.orders[id] = "placed"
	return id
}

// reserve decrements stock exactly once per order: workflow steps run
// at-least-once, so the reservation is keyed by order id to stay idempotent.
func (s *shop) reserve(orderID, item string, qty int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved[orderID] {
		return nil
	}
	if s.stock[item] < qty {
		return fmt.Errorf("item %q is out of stock", item)
	}
	s.stock[item] -= qty
	s.reserved[orderID] = true
	return nil
}

// release returns an order's reserved stock; a no-op if nothing is reserved.
func (s *shop) release(orderID, item string, qty int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved[orderID] {
		s.stock[item] += qty
		s.reserved[orderID] = false
	}
}

func (s *shop) setStatus(orderID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orders[orderID] = status
}

func (s *shop) counts() (placed, fulfilled, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, status := range s.orders {
		switch status {
		case "fulfilled":
			fulfilled++
		case "failed":
			failed++
		default:
			placed++
		}
	}
	return placed, fulfilled, failed
}
