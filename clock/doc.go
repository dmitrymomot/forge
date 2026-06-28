// Package clock is the framework's testable time seam. Production code accepts a
// Clock and calls Now instead of time.Now, so expiry and scheduling logic can be
// driven deterministically in tests via Mock.
//
//	type svc struct{ clk clock.Clock }
//	func newSvc() *svc { return &svc{clk: clock.System()} }
//
//	// in a test:
//	m := clock.NewMock(time.Unix(0, 0))
//	s := &svc{clk: m}
//	m.Advance(time.Hour) // s now sees time one hour later
//
// Only Now is provided today. Timer/ticker helpers are deferred until the async
// layer needs them; adding them later is additive.
package clock
