// Package parallel runs work concurrently with a bounded worker count.
//
//	squares, err := parallel.Map(ctx, ids, 8, func(ctx context.Context, id int) (int, error) {
//	    return id * id, nil
//	})
//
// For imperative use, construct a Group:
//
//	g, ctx := parallel.New(ctx, parallel.WithLimit(4))
//	for _, job := range jobs {
//	    g.Go(func(ctx context.Context) error { return job.Run(ctx) })
//	}
//	err := g.Wait()
package parallel
