package supervisor

import "context"

// fakeService is a controllable Service test double shared across test files.
type fakeService struct {
	name string
	run  func(ctx context.Context) error
}

func (f fakeService) Name() string                  { return f.name }
func (f fakeService) Run(ctx context.Context) error { return f.run(ctx) }
