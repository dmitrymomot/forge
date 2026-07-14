package fsm_test

import (
	"context"
	"errors"
	"sync"
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

// record returns a Func that logs its name; it fails when name == failOn.
func record(log *[]string, name string, failOn string) fsm.Func[status, *task] {
	return func(ctx context.Context, v *task, from, to status) error {
		*log = append(*log, name)
		if name == failOn {
			return errors.New(name + " says no")
		}
		return nil
	}
}

// newOrderMachine wires one guard and one hook onto every surface of the
// review -> done edge so ordering is observable.
func newOrderMachine(t *testing.T, log *[]string, failOn string) *fsm.Machine[status, *task] {
	t.Helper()
	var d fsm.Define[status, *task]
	m, err := fsm.New(statusReview,
		d.Edge(statusReview, statusDone,
			d.Guard(record(log, "edge-guard", failOn)),
			d.Hook(record(log, "edge-hook", failOn)),
		),
		d.OnEnter(statusDone,
			d.Guard(record(log, "enter-guard", failOn)),
			d.Hook(record(log, "enter-hook", failOn)),
		),
		d.OnExit(statusReview,
			d.Guard(record(log, "exit-guard", failOn)),
			d.Hook(record(log, "exit-hook", failOn)),
		),
	)
	require.NoError(t, err)
	return m
}

func TestFire_GuardHookOrdering(t *testing.T) {
	var log []string
	m := newOrderMachine(t, &log, "")
	require.NoError(t, m.Fire(context.Background(), &task{}, statusReview, statusDone))
	assert.Equal(t, []string{
		"exit-guard", "edge-guard", "enter-guard",
		"exit-hook", "edge-hook", "enter-hook",
	}, log, "guards exit->edge->enter, then hooks exit->edge->enter")
}

func TestFire_GuardDenialAbortsBeforeAnyHook(t *testing.T) {
	var log []string
	m := newOrderMachine(t, &log, "enter-guard")
	err := m.Fire(context.Background(), &task{}, statusReview, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrGuardDenied))
	assert.Equal(t, []string{"exit-guard", "edge-guard", "enter-guard"}, log, "zero hooks ran")
}

func TestFire_FirstGuardFailureShortCircuits(t *testing.T) {
	var log []string
	m := newOrderMachine(t, &log, "exit-guard")
	err := m.Fire(context.Background(), &task{}, statusReview, statusDone)
	require.Error(t, err)
	assert.Equal(t, []string{"exit-guard"}, log)
}

func TestFire_HookFailureAborts(t *testing.T) {
	var log []string
	m := newOrderMachine(t, &log, "edge-hook")
	err := m.Fire(context.Background(), &task{}, statusReview, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrHookFailed))
	assert.Equal(t, []string{
		"exit-guard", "edge-guard", "enter-guard",
		"exit-hook", "edge-hook",
	}, log, "enter-hook never ran")
}

var errDomainDenied = errors.New("3 subtasks still open")

func TestFire_GuardErrorSurvivesDoubleWrap(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusReview,
		d.Edge(statusReview, statusDone, d.Guard(func(ctx context.Context, v *task, from, to status) error {
			return errDomainDenied
		})),
	)
	err := m.Fire(context.Background(), &task{}, statusReview, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrGuardDenied), "sentinel matches")
	assert.True(t, errors.Is(err, errDomainDenied), "domain error survives")
	assert.Contains(t, err.Error(), "3 subtasks still open")
	assert.NotContains(t, err.Error(), "\n", "single-line error")
}

func TestFire_HookErrorSurvivesDoubleWrap(t *testing.T) {
	errBoom := errors.New("stamp exploded")
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusReview,
		d.Edge(statusReview, statusDone, d.Hook(func(ctx context.Context, v *task, from, to status) error {
			return errBoom
		})),
	)
	err := m.Fire(context.Background(), &task{}, statusReview, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrHookFailed))
	assert.True(t, errors.Is(err, errBoom))
}

func TestFire_HookMutatesEntity(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusReview,
		d.Edge(statusReview, statusDone),
		d.OnEnter(statusDone, d.Hook(func(ctx context.Context, v *task, from, to status) error {
			v.completedAt = "2026-07-14"
			return nil
		})),
	)
	v := &task{}
	require.NoError(t, m.Fire(context.Background(), v, statusReview, statusDone))
	assert.Equal(t, "2026-07-14", v.completedAt)
}

func TestFire_GuardInspectsEntity(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusReview,
		d.Edge(statusReview, statusDone, d.Guard(func(ctx context.Context, v *task, from, to status) error {
			if v.openSubtasks > 0 {
				return errors.New("subtasks open")
			}
			return nil
		})),
	)
	require.Error(t, m.Fire(context.Background(), &task{openSubtasks: 2}, statusReview, statusDone))
	require.NoError(t, m.Fire(context.Background(), &task{}, statusReview, statusDone))
}

// newAdminBoard: done -> open guarded by admin-only; cancel from anywhere.
func newAdminBoard(t *testing.T) *fsm.Machine[status, *task] {
	t.Helper()
	var d fsm.Define[status, *task]
	m, err := fsm.New(statusOpen,
		d.Edge(statusOpen, statusDone),
		d.Edge(statusDone, statusOpen, d.Guard(func(ctx context.Context, v *task, from, to status) error {
			if !v.admin {
				return errors.New("only admins may reopen")
			}
			return nil
		})),
		d.EdgeFromAny(statusCancelled),
	)
	require.NoError(t, err)
	return m
}

func TestAllowed_FiltersByGuards(t *testing.T) {
	m := newAdminBoard(t)

	got, err := m.Allowed(context.Background(), &task{admin: false}, statusDone)
	require.NoError(t, err)
	assert.Equal(t, []status{statusCancelled}, got, "non-admin cannot reopen")

	got, err = m.Allowed(context.Background(), &task{admin: true}, statusDone)
	require.NoError(t, err)
	assert.Equal(t, []status{statusOpen, statusCancelled}, got, "admin sees reopen, declaration order")
}

func TestAllowed_UnknownState(t *testing.T) {
	m := newAdminBoard(t)
	_, err := m.Allowed(context.Background(), &task{}, status("nope"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrUnknownState))
}

func TestAllowed_CancelledContextIsAnError(t *testing.T) {
	m := newAdminBoard(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := m.Allowed(ctx, &task{admin: true}, statusDone)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	assert.Nil(t, got, "a dead context must not read as 'no moves possible'")
}

func TestAllowed_EmptyResultIsNotAnError(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone, d.Guard(func(ctx context.Context, v *task, from, to status) error {
			return errors.New("always denied")
		})),
	)
	got, err := m.Allowed(context.Background(), &task{}, statusOpen)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestAllowed_RunsNoHooks(t *testing.T) {
	var log []string
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone, d.Hook(record(&log, "edge-hook", ""))),
		d.OnEnter(statusDone, d.Hook(record(&log, "enter-hook", ""))),
	)
	_, err := m.Allowed(context.Background(), &task{}, statusOpen)
	require.NoError(t, err)
	assert.Empty(t, log, "Allowed evaluates guards only")
}

func TestFire_ZeroAllocsOnBareEdge(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen, d.Edge(statusOpen, statusDone))
	v := &task{}
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		if err := m.Fire(ctx, v, statusOpen, statusDone); err != nil {
			t.Fatal(err)
		}
	})
	assert.Zero(t, allocs, "bare Fire is the hot path: 0 allocs/op")
}

func TestMachine_ConcurrentUse(t *testing.T) {
	var d fsm.Define[status, *task]
	m := fsm.MustNew(statusOpen,
		d.Edge(statusOpen, statusDone, d.Guard(func(ctx context.Context, v *task, from, to status) error {
			if v.openSubtasks > 0 {
				return errors.New("subtasks open")
			}
			return nil
		})),
		d.EdgeFromAny(statusCancelled),
	)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			v := &task{}
			for range 100 {
				_ = m.Fire(context.Background(), v, statusOpen, statusDone)
				_, _ = m.Allowed(context.Background(), v, statusOpen)
				_ = m.Next(statusOpen)
				_ = m.States()
			}
		})
	}
	wg.Wait()
}
