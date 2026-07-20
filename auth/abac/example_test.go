package abac_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/abac"
	"github.com/dmitrymomot/forge/auth/access"
)

// The catalog scenario: an agent sees agents in its own subtree but not its
// subagents' player details. The relationship data (the agent tree) stays in
// consumer code feeding the predicates.
func Example() {
	parent := map[string]string{"sub1": "agent1", "sub2": "sub1"}
	inSubtree := func(root, node string) bool {
		for node != "" {
			if node == root {
				return true
			}
			node = parent[node]
		}
		return false
	}

	policy, err := abac.New(
		abac.Allow("own-subtree", "agents:read", "agent",
			func(_ context.Context, s access.Subject, r access.Resource) (bool, error) {
				return inSubtree(s.ID, r.ID), nil
			}),
		abac.Deny("subagent-player-details", "players:read", "player",
			func(_ context.Context, s access.Subject, r access.Resource) (bool, error) {
				agentID, _ := abac.Attr[string](r.Attrs, "agent_id")
				return agentID != s.ID && inSubtree(s.ID, agentID), nil
			}),
	)
	if err != nil {
		panic(err)
	}

	agent := access.Subject{ID: "agent1"}

	dec, _ := access.Authorize(context.Background(), policy, agent,
		"agents:read", access.Resource{Type: "agent", ID: "sub2"})
	fmt.Println(dec.Effect, "—", dec.Reason)

	dec, _ = access.Authorize(context.Background(), policy, agent,
		"players:read", access.Resource{Type: "player", ID: "p1", Attrs: map[string]any{"agent_id": "sub1"}})
	fmt.Println(dec.Effect, "—", dec.Reason)

	// Output:
	// allow — allowed by rule "own-subtree"
	// deny — denied by rule "subagent-player-details"
}
