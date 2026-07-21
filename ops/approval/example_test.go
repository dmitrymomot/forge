package approval_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/auth/access"
	"github.com/dmitrymomot/forge/ops/approval"
)

type releasePayout struct {
	PayoutID string `json:"payout_id"`
	Amount   int64  `json:"amount"`
}

var kindRelease = approval.NewKind[releasePayout]("payout.release")

func Example() {
	m := approval.New(approval.NewMemoryStore(),
		approval.WithKind(kindRelease, approval.Policy{Quorum: 2}))
	ctx := context.Background()

	// The maker submits.
	req, err := approval.Submit(ctx, m, kindRelease,
		releasePayout{PayoutID: "po_88", Amount: 250000},
		approval.SubmitParams{Requester: "alice", Reason: "vendor invoice #4471"})
	if err != nil {
		panic(err)
	}
	fmt.Println("submitted:", req.Status)

	// The maker cannot be a checker.
	_, err = m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "alice"}})
	fmt.Println("self-approval:", err)

	// Two checkers approve.
	got, _ := m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "bob"}})
	fmt.Println("one of two:", got.Status)
	got, _ = m.Approve(ctx, req.ID, approval.Actor{Subject: access.Subject{ID: "carol"}})
	fmt.Println("two of two:", got.Status)

	// Exactly one executor runs the action.
	done, err := m.Execute(ctx, req.ID, "worker-1", func(ctx context.Context, r approval.Request) error {
		p, err := approval.PayloadOf(kindRelease, r)
		if err != nil {
			return err
		}
		fmt.Println("releasing:", p.PayoutID)
		return nil
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("finished:", done.Status)

	// Output:
	// submitted: pending
	// self-approval: approval: requester cannot decide own request
	// one of two: pending
	// two of two: approved
	// releasing: po_88
	// finished: executed
}
