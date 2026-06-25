package supervisor

import "context"

// Service is a long-running unit of work supervised by Run.
//
// Name returns a non-empty, stable identifier used in logs and shutdown
// diagnostics. Run blocks until the work completes or ctx is cancelled;
// implementations MUST observe ctx cancellation and shut down gracefully,
// returning when drained. Returning context.Canceled in response to
// cancellation is treated as a clean stop.
type Service interface {
	Name() string
	Run(ctx context.Context) error
}

// serviceFunc adapts a named function to Service. Created by WithServiceFunc.
type serviceFunc struct {
	name string
	fn   func(ctx context.Context) error
}

func (s serviceFunc) Name() string                  { return s.name }
func (s serviceFunc) Run(ctx context.Context) error { return s.fn(ctx) }
