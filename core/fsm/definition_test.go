package fsm_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/fsm"
)

type move struct {
	task  *task
	admin bool
}

func testRegistry(log *[]string) fsm.Registry[*move] {
	return fsm.Registry[*move]{
		Guards: map[string]fsm.Func[string, *move]{
			"admin_only": func(ctx context.Context, m *move, from, to string) error {
				if !m.admin {
					return errors.New("only admins may do that")
				}
				return nil
			},
			"subtasks_closed": func(ctx context.Context, m *move, from, to string) error {
				if m.task.openSubtasks > 0 {
					return errors.New("subtasks still open")
				}
				return nil
			},
		},
		Hooks: map[string]fsm.Func[string, *move]{
			"stamp_completed": func(ctx context.Context, m *move, from, to string) error {
				m.task.completedAt = "2026-07-14"
				*log = append(*log, "stamp_completed")
				return nil
			},
		},
	}
}

func flowDefinition() fsm.Definition {
	return fsm.Definition{
		Initial: "open",
		States: []fsm.StateDef{
			{Name: "open"},
			{Name: "in_progress"},
			{Name: "done", OnEnterGuards: []string{"subtasks_closed"}, OnEnterHooks: []string{"stamp_completed"}},
			{Name: "cancelled"},
		},
		Edges: []fsm.EdgeDef{
			{From: "open", To: "in_progress"},
			{From: "in_progress", To: "done"},
			{From: "*", To: "cancelled"},
			{From: "done", To: "open", Guards: []string{"admin_only"}},
		},
	}
}

func TestCompile_HappyPath(t *testing.T) {
	var log []string
	m, err := fsm.Compile(flowDefinition(), testRegistry(&log))
	require.NoError(t, err)

	assert.Equal(t, "open", m.Initial())
	assert.Equal(t, []string{"open", "in_progress", "done", "cancelled"}, m.States())

	// Guard denies, then passes; enter-hook stamps.
	v := &move{task: &task{openSubtasks: 1}}
	err = m.Fire(context.Background(), v, "in_progress", "done")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrGuardDenied))

	v = &move{task: &task{}}
	require.NoError(t, m.Fire(context.Background(), v, "in_progress", "done"))
	assert.Equal(t, "2026-07-14", v.task.completedAt)
	assert.Equal(t, []string{"stamp_completed"}, log)

	// Wildcard expanded; admin-guarded reopen filters in Allowed.
	assert.True(t, m.Can("open", "cancelled"))
	assert.False(t, m.Can("cancelled", "cancelled"))

	got, err := m.Allowed(context.Background(), &move{task: &task{}}, "done")
	require.NoError(t, err)
	assert.Equal(t, []string{"cancelled"}, got)

	got, err = m.Allowed(context.Background(), &move{task: &task{}, admin: true}, "done")
	require.NoError(t, err)
	assert.Equal(t, []string{"cancelled", "open"}, got, "wildcard declared before the reopen edge")
}

func TestCompile_EmptyStateSet(t *testing.T) {
	_, err := fsm.Compile(fsm.Definition{Initial: "open"}, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrInvalidDefinition))
	assert.Contains(t, err.Error(), "empty state set")
}

func TestCompile_DuplicateState(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States:  []fsm.StateDef{{Name: "open"}, {Name: "open"}},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `duplicate state "open"`)
}

func TestCompile_InitialNotDeclared(t *testing.T) {
	def := fsm.Definition{
		Initial: "missing",
		States:  []fsm.StateDef{{Name: "open"}},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `initial state "missing" not declared`)
}

func TestCompile_EdgeReferencesUndeclaredState(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States:  []fsm.StateDef{{Name: "open"}},
		Edges:   []fsm.EdgeDef{{From: "open", To: "ghost"}, {From: "phantom", To: "open"}},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `edge references undeclared state "ghost"`)
	assert.Contains(t, err.Error(), `edge references undeclared state "phantom"`)
}

func TestCompile_UnknownGuardAndHookNames(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States: []fsm.StateDef{
			{Name: "open", OnExitHooks: []string{"no_such_hook"}},
			{Name: "done"},
		},
		Edges: []fsm.EdgeDef{{From: "open", To: "done", Guards: []string{"no_such_guard"}}},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown guard "no_such_guard"`)
	assert.Contains(t, err.Error(), `unknown hook "no_such_hook"`)
}

func TestCompile_WhitespaceStateName(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States:  []fsm.StateDef{{Name: "open"}, {Name: "done "}},
		Edges:   []fsm.EdgeDef{{From: "open", To: "done "}},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "leading or trailing whitespace")
}

func TestCompile_AllIssuesReportedAtOnce(t *testing.T) {
	def := fsm.Definition{
		Initial: "missing",
		States:  []fsm.StateDef{{Name: "open"}, {Name: "open"}},
		Edges: []fsm.EdgeDef{
			{From: "open", To: "ghost"},
			{From: "open", To: "open", Guards: []string{"no_such_guard"}},
		},
	}
	_, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsm.ErrInvalidDefinition))
	assert.Contains(t, err.Error(), `duplicate state "open"`)
	assert.Contains(t, err.Error(), `initial state "missing" not declared`)
	assert.Contains(t, err.Error(), `edge references undeclared state "ghost"`)
	assert.Contains(t, err.Error(), `unknown guard "no_such_guard"`)
	assert.NotContains(t, err.Error(), "\n", "single-line aggregate")
}

func TestCompile_NilRegistryMapsValidWithoutNames(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States:  []fsm.StateDef{{Name: "open"}, {Name: "done"}},
		Edges:   []fsm.EdgeDef{{From: "open", To: "done"}},
	}
	m, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.NoError(t, err)
	require.NoError(t, m.Fire(context.Background(), &move{task: &task{}}, "open", "done"))
}

func TestDefinition_JSONRoundTrip(t *testing.T) {
	def := flowDefinition()
	raw, err := json.Marshal(def)
	require.NoError(t, err)

	var back fsm.Definition
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, def, back)

	assert.Contains(t, string(raw), `"on_enter_guards"`)
	assert.NotContains(t, string(raw), `"on_exit_guards"`, "omitempty keeps unset lists out")
}

func TestCompile_DeclaredStateWithNoEdgesIsUsable(t *testing.T) {
	def := fsm.Definition{
		Initial: "open",
		States:  []fsm.StateDef{{Name: "open"}, {Name: "parked"}, {Name: "done"}},
		Edges:   []fsm.EdgeDef{{From: "parked", To: "done"}, {From: "open", To: "done"}},
	}
	m, err := fsm.Compile(def, fsm.Registry[*move]{})
	require.NoError(t, err)
	// "parked" is unreachable from initial but still a valid source: reachability is not enforced.
	require.NoError(t, m.Fire(context.Background(), &move{task: &task{}}, "parked", "done"))
}
