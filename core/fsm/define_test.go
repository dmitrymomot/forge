package fsm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/fsm"
)

func TestNew_DuplicateEdge(t *testing.T) {
	var d fsm.Define[status, *task]
	_, err := fsm.New(statusOpen,
		d.Edge(statusOpen, statusDone),
		d.Edge(statusOpen, statusDone),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrInvalidDefinition))
	assert.Contains(t, err.Error(), "duplicate edge open -> done")
}

func TestNew_InvalidStateNames(t *testing.T) {
	var d fsm.Define[status, *task]
	_, err := fsm.New(statusOpen,
		d.Edge(statusOpen, status("")),
		d.Edge(statusOpen, status("*")),
		d.Edge(statusOpen, status("done ")),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrInvalidDefinition))
	assert.Contains(t, err.Error(), "empty state name")
	assert.Contains(t, err.Error(), `state named "*"`)
	assert.Contains(t, err.Error(), "leading or trailing whitespace")
}

func TestNew_IssuesAggregateSingleLine(t *testing.T) {
	var d fsm.Define[status, *task]
	_, err := fsm.New(statusOpen,
		d.Edge(statusOpen, status("")),
		d.Edge(statusOpen, statusDone),
		d.Edge(statusOpen, statusDone),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "; ", "issues joined on one line")
	assert.NotContains(t, err.Error(), "\n", "single-line error")
}

func TestNew_InternalSpacesLegal(t *testing.T) {
	var d fsm.Define[status, *task]
	_, err := fsm.New(status("In Progress"), d.Edge(status("In Progress"), statusDone))
	require.NoError(t, err)
}

func TestMustNew_PanicsOnInvalid(t *testing.T) {
	var d fsm.Define[status, *task]
	assert.Panics(t, func() {
		fsm.MustNew(statusOpen, d.Edge(statusOpen, statusDone), d.Edge(statusOpen, statusDone))
	})
}

func TestMustNew_ReturnsMachine(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen, d.Edge(statusOpen, statusDone))
	assert.True(t, m.Can(statusOpen, statusDone))
}

func TestOnEnter_MultiplePartsAppendInOrder(t *testing.T) {
	var log []string
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusReview,
		d.Edge(statusReview, statusDone),
		d.OnEnter(statusDone, d.Hook(record(&log, "first", ""))),
		d.OnEnter(statusDone, d.Hook(record(&log, "second", ""))),
	)
	require.NoError(t, m.Fire(context.Background(), &task{}, statusReview, statusDone))
	assert.Equal(t, []string{"first", "second"}, log)
}

func TestOnEnterOnExit_DeclareTheirState(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone),
		d.OnExit(statusCancelled), // mentions a state with no edges
	)
	assert.Contains(t, m.States(), statusCancelled)
}
