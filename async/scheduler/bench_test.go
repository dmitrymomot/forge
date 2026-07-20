package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/scheduler"
)

func BenchmarkCronParse(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := scheduler.Cron("*/15 9-17 * * mon-fri"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCronNext(b *testing.B) {
	from := time.Date(2026, 7, 20, 10, 7, 30, 0, time.UTC)
	cases := []struct {
		name string
		spec string
	}{
		{"every_minute", "* * * * *"},
		{"business_hours", "*/15 9-17 * * mon-fri"},
		{"monthly", "0 3 1 * *"},
		{"leap_day", "0 0 29 2 *"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			sched := scheduler.MustCron(tc.spec)
			b.ReportAllocs()
			for b.Loop() {
				if sched.Next(from).IsZero() {
					b.Fatal("unexpected zero next")
				}
			}
		})
	}
}

func BenchmarkEveryNext(b *testing.B) {
	sched := scheduler.Every(15 * time.Minute)
	from := time.Date(2026, 7, 20, 10, 7, 30, 0, time.UTC)
	b.ReportAllocs()
	for b.Loop() {
		if sched.Next(from).IsZero() {
			b.Fatal("unexpected zero next")
		}
	}
}

func BenchmarkMemoryStoreClaim(b *testing.B) {
	st := scheduler.NewMemoryStore()
	ctx := context.Background()
	base := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	b.Run("first_claim", func(b *testing.B) {
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			i++
			if err := st.Claim(ctx, "bench", base.Add(time.Duration(i)*time.Second)); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("duplicate", func(b *testing.B) {
		if err := st.Claim(ctx, "bench-dup", base); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if err := st.Claim(ctx, "bench-dup", base); err == nil {
				b.Fatal("expected ErrAlreadyClaimed")
			}
		}
	})
}
