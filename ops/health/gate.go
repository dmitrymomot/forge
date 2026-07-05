package health

import (
	"context"
	"sync/atomic"
)

// Gate is a flippable readiness signal exposed as a Check — the drain primitive.
// It starts up (healthy); Down makes its Check report ErrDraining.
type Gate struct {
	up atomic.Bool
}

// NewGate returns a Gate in the "up" state.
func NewGate() *Gate {
	g := &Gate{}
	g.up.Store(true)
	return g
}

// Check reports nil while up, ErrDraining once Down.
func (g *Gate) Check(context.Context) error {
	if g.up.Load() {
		return nil
	}
	return ErrDraining
}

// Up marks the gate healthy.
func (g *Gate) Up() { g.up.Store(true) }

// Down marks the gate draining.
func (g *Gate) Down() { g.up.Store(false) }
