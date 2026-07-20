package collector_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/async/collector"
)

func Example() {
	sink := collector.SinkFunc[string](func(_ context.Context, batch []string) error {
		fmt.Println("flushed:", batch)
		return nil
	})
	c, err := collector.New(sink)
	if err != nil {
		panic(err)
	}

	// In the request path: buffer and move on, never block.
	_ = c.Add(context.Background(), "click:/pricing")
	_ = c.Add(context.Background(), "click:/docs")

	// Run under ops/supervisor in production; a cancelled context here shows
	// the graceful drain: buffered events flush before Run returns.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = c.Run(ctx)

	// Output:
	// flushed: [click:/pricing click:/docs]
}
