package dataloader_test

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/dmitrymomot/forge/data/dataloader"
)

func ExampleLoader() {
	// The BatchFunc owns the lookup — one round trip for the whole batch.
	users := map[string]string{"u1": "Ada", "u2": "Grace", "u3": "Edsger"}
	loader := dataloader.New(func(_ context.Context, ids []string) (map[string]string, error) {
		fmt.Printf("one fetch for %d ids\n", len(ids))
		out := make(map[string]string, len(ids))
		for _, id := range ids {
			if name, ok := users[id]; ok {
				out[id] = name
			}
		}
		return out, nil
	}, dataloader.WithMaxBatchSize(3))

	// A loop's N lookups collapse into one BatchFunc call.
	names, err := loader.LoadMany(context.Background(), []string{"u1", "u2", "u3"})
	if err != nil {
		fmt.Println("load:", err)
		return
	}
	for _, id := range slices.Sorted(maps.Keys(names)) {
		fmt.Println(id, "->", names[id])
	}

	// Resolved keys are memoized: no second fetch.
	name, _ := loader.Load(context.Background(), "u2")
	fmt.Println("cached:", name)

	// Output:
	// one fetch for 3 ids
	// u1 -> Ada
	// u2 -> Grace
	// u3 -> Edsger
	// cached: Grace
}
