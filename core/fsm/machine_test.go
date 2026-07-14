package fsm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/fsm"
)

type status string

const (
	statusOpen       status = "open"
	statusInProgress status = "in_progress"
	statusReview     status = "review"
	statusDone       status = "done"
	statusCancelled  status = "cancelled"
)

type task struct {
	completedAt  string
	openSubtasks int
	admin        bool
}

// newBoard compiles the reference flow used across tests:
// open -> in_progress -> review -> (in_progress | done).
func newBoard(t *testing.T) *fsm.Machine[status, *task] {
	t.Helper()
	var d fsm.Define[status, *task]
	m, err := fsm.New(statusOpen,
		d.Edge(statusOpen, statusInProgress),
		d.Edge(statusInProgress, statusReview),
		d.Edge(statusReview, statusInProgress),
		d.Edge(statusReview, statusDone),
	)
	require.NoError(t, err)
	return m
}

func TestFire_LegalTransition(t *testing.T) {
	m := newBoard(t)
	require.NoError(t, m.Fire(context.Background(), &task{}, statusOpen, statusInProgress))
}

func TestFire_IllegalTransition(t *testing.T) {
	m := newBoard(t)
	err := m.Fire(context.Background(), &task{}, statusOpen, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrIllegalTransition))
	assert.Contains(t, err.Error(), "open")
	assert.Contains(t, err.Error(), "done")
}

func TestFire_UnknownState(t *testing.T) {
	m := newBoard(t)

	err := m.Fire(context.Background(), &task{}, status("nope"), statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrUnknownState))

	err = m.Fire(context.Background(), &task{}, statusOpen, status("nope"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrUnknownState))
}

func TestFire_SelfTransitionNeedsExplicitEdge(t *testing.T) {
	m := newBoard(t)
	err := m.Fire(context.Background(), &task{}, statusOpen, statusOpen)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrIllegalTransition))
}

func TestFire_ExplicitSelfEdgeAllowed(t *testing.T) {
	var d fsm.Define[status, *task]
	m, err := fsm.New(statusOpen, d.Edge(statusOpen, statusOpen))
	require.NoError(t, err)
	require.NoError(t, m.Fire(context.Background(), &task{}, statusOpen, statusOpen))
}

func TestCan(t *testing.T) {
	m := newBoard(t)
	assert.True(t, m.Can(statusOpen, statusInProgress))
	assert.False(t, m.Can(statusOpen, statusDone))
	assert.False(t, m.Can(status("nope"), statusDone))
}

func TestNext_DeclarationOrder(t *testing.T) {
	m := newBoard(t)
	assert.Equal(t, []status{statusInProgress, statusDone}, m.Next(statusReview))
	assert.Nil(t, m.Next(statusDone), "no outgoing edges")
	assert.Nil(t, m.Next(status("nope")), "unknown state")
}

func TestNext_ReturnsCopy(t *testing.T) {
	m := newBoard(t)
	next := m.Next(statusReview)
	next[0] = "mutated"
	assert.Equal(t, statusInProgress, m.Next(statusReview)[0])
}

func TestStatesAndInitial(t *testing.T) {
	m := newBoard(t)
	assert.Equal(t, statusOpen, m.Initial())
	assert.Equal(t, []status{statusOpen, statusInProgress, statusReview, statusDone}, m.States(), "first-mention order")

	states := m.States()
	states[0] = "mutated"
	assert.Equal(t, statusOpen, m.States()[0], "States() returns an independent copy")
}
