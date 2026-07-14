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

// newWildcardBoard: open -> in_progress -> review -> done, plus cancel-from-anywhere;
// done -> cancelled is explicitly replaced with an extra guard.
func newWildcardBoard(t *testing.T, log *[]string) *fsm.Machine[status, *task] {
	t.Helper()
	var d fsm.Define[status, *task]
	m, err := fsm.New(statusOpen,
		d.Edge(statusOpen, statusInProgress),
		d.Edge(statusInProgress, statusReview),
		d.Edge(statusReview, statusDone),
		d.EdgeFromAny(statusCancelled, d.Guard(record(log, "wildcard-guard", ""))),
		d.Edge(statusDone, statusCancelled, d.Guard(record(log, "explicit-guard", ""))),
	)
	require.NoError(t, err)
	return m
}

func TestEdgeFromAny_ExpandsToAllStates(t *testing.T) {
	m := newWildcardBoard(t, new([]string))
	for _, from := range []status{statusOpen, statusInProgress, statusReview, statusDone} {
		assert.True(t, m.Can(from, statusCancelled), "from %s", from)
	}
}

func TestEdgeFromAny_NoSelfLoop(t *testing.T) {
	m := newWildcardBoard(t, new([]string))
	assert.False(t, m.Can(statusCancelled, statusCancelled))
}

func TestEdgeFromAny_WildcardGuardAppliesToExpandedEdges(t *testing.T) {
	var log []string
	m := newWildcardBoard(t, &log)
	require.NoError(t, m.Fire(context.Background(), &task{}, statusOpen, statusCancelled))
	assert.Equal(t, []string{"wildcard-guard"}, log)
}

func TestEdgeFromAny_ExplicitEdgeReplacesExpanded(t *testing.T) {
	var log []string
	m := newWildcardBoard(t, &log)
	require.NoError(t, m.Fire(context.Background(), &task{}, statusDone, statusCancelled))
	assert.Equal(t, []string{"explicit-guard"}, log, "wildcard guard must NOT run on the replaced pair")
}

func TestEdgeFromAny_DuplicateWildcard(t *testing.T) {
	var d fsm.Define[status, *task]
	_, err := fsm.New(statusOpen,
		d.Edge(statusOpen, statusDone),
		d.EdgeFromAny(statusCancelled),
		d.EdgeFromAny(statusCancelled),
	)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrInvalidDefinition))
	assert.Contains(t, err.Error(), "duplicate edge * -> cancelled")
}

func TestEdgeFromAny_NextKeepsDeclarationPosition(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusInProgress),
		d.EdgeFromAny(statusCancelled),
		d.Edge(statusOpen, statusReview),
	)
	assert.Equal(t, []status{statusInProgress, statusCancelled, statusReview}, m.Next(statusOpen), "wildcard expands at its declaration position")
}

func TestEdgeFromAny_DeclaresItsTarget(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone),
		d.EdgeFromAny(statusCancelled),
	)
	assert.Contains(t, m.States(), statusCancelled)
}
