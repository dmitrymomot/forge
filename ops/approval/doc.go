// Package approval implements maker-checker dual control: a privileged
// action is recorded as a request, a second person approves or rejects it,
// and exactly one executor carries it out.
//
//	var KindReleasePayout = approval.NewKind[ReleasePayout]("payout.release")
//
//	m := approval.New(approval.NewMemoryStore(), // pgstore.New(pool) in production
//		approval.WithKind(KindReleasePayout, approval.Policy{Quorum: 2, TTL: 24 * time.Hour}))
//
//	req, err := approval.Submit(ctx, m, KindReleasePayout,
//		ReleasePayout{PayoutID: "po_88", Amount: amount},
//		approval.SubmitParams{Requester: "alice", Reason: "vendor invoice #4471"})
//
// Checkers vote with Approve and Reject. The quorum-th distinct approval
// moves the request to Approved; a single rejection is terminal.
//
//	req, err := m.Approve(ctx, req.ID, approval.Actor{
//		Subject: access.Subject{ID: "bob"}, Reason: "matched to invoice"})
//
// Three rules are structural and cannot be configured off: the requester
// may never decide their own request (at any quorum — Quorum 1 is still
// dual control), a second decision from an approver who already voted is
// rejected with ErrAlreadyVoted, and a rejected request never becomes
// approved.
//
// # Executing once
//
// Reaching Approved does not run anything. For actions where running twice
// is the real danger — releasing a payout, booking a calendar entry — claim
// the request first: Claim moves it to Executing atomically, so two
// operators or two racing webhook deliveries produce one winner and one
// ErrAlreadyClaimed.
//
//	done, err := m.Execute(ctx, req.ID, "worker-1", func(ctx context.Context, r approval.Request) error {
//		p, err := approval.PayloadOf(KindReleasePayout, r)
//		if err != nil {
//			return err
//		}
//		return payouts.Release(ctx, p.PayoutID, p.Amount)
//	})
//
// Execute wraps Claim, then Complete or Fail. Call them directly when the
// action spans processes. By default a claim never expires, so an executor
// that dies mid-action wedges the request until Release is called — the
// safe default for money. Policy.ClaimTTL opts into automatic takeover, and
// with it the action must be idempotent.
//
// Requests that nobody acts on expire after Policy.TTL. Expiry is derived
// on read, not swept: a Pending or Approved request past its deadline
// reports Expired and refuses every transition, with no background
// goroutine anywhere.
//
// # Eligibility, audit, and tenancy
//
// WithDecider gates who may vote through the auth/access decision seam,
// asking "<kind>:decide" with the request's kind, requester, and raw
// payload as attributes — enough for relational rules ("must be the
// requester's manager") and value-aware ones. It fails closed: a decider
// that errors refuses the vote.
//
// WithAuditor writes every transition to an ops/auditlog Recorder,
// including refused decision attempts — decider denials, self-approvals,
// and duplicate votes. WithScope confines every operation
// to a context-derived tenant, failing closed when none is available;
// single-tenant applications pay no ceremony.
//
//	m := approval.New(store,
//		approval.WithKind(KindReleasePayout, approval.Policy{Quorum: 2}),
//		approval.WithDecider(roles),
//		approval.WithAuditor(rec),
//		approval.WithScope(tenantFromContext))
//
// State lives behind the Store interface. NewMemoryStore is for tests and
// development; approval/pgstore persists to Postgres.
package approval
