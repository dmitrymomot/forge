//go:build integration

package pgoutbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func benchJobs(n int) []queue.Job {
	now := time.Now().UTC()
	jobs := make([]queue.Job, n)
	for i := range jobs {
		jobs[i] = makeJob(now.Add(time.Duration(i) * time.Microsecond))
	}
	return jobs
}

func BenchmarkPgOutbox_Add100(b *testing.B) {
	pool := openPool(b)
	s := newStore(b, pool)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		jobs := benchJobs(100)
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return s.Add(ctx, tx, jobs...)
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPgOutbox_ClaimDelete100(b *testing.B) {
	pool := openPool(b)
	s := newStore(b, pool)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		jobs := benchJobs(100)
		require.NoError(b, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return s.Add(ctx, tx, jobs...)
		}))
		// Rows are visible once available_at (stamped by the DB clock at
		// insert) is in the past; a single claim right after commit sees them.
		b.StartTimer()

		claimed := 0
		for claimed < 100 {
			got, err := s.Claim(ctx, 100, time.Minute)
			if err != nil {
				b.Fatal(err)
			}
			if len(got) == 0 {
				continue
			}
			ids := make([]string, len(got))
			for i, e := range got {
				ids[i] = e.Job.ID
			}
			if err := s.Delete(ctx, ids...); err != nil {
				b.Fatal(err)
			}
			claimed += len(got)
		}
	}
}
