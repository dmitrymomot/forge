// ops/featureflag/bench_test.go
package featureflag_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/ops/featureflag"
)

func benchClient(b *testing.B, opts ...featureflag.Option) *featureflag.Client {
	b.Helper()
	c, err := featureflag.New(opts...)
	if err != nil {
		b.Fatal(err)
	}
	return c
}

func BenchmarkBool_StaticHit(b *testing.B) {
	c := benchClient(b, featureflag.WithBool("f", true))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if !c.Bool(ctx, "f", false) {
			b.Fatal("expected true")
		}
	}
}

func BenchmarkBool_StaticHit_Parallel(b *testing.B) {
	c := benchClient(b, featureflag.WithBool("f", true))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = c.Bool(ctx, "f", false)
		}
	})
}

func BenchmarkBool_Rollout(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 50),
	)
	ctx := featureflag.WithSubject(context.Background(), "usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "f", false)
	}
}

func BenchmarkBool_TokenMatch(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 0),
		featureflag.WithAllow("f", "segment:vip", "role:staff"),
		featureflag.WithIdentity(func(context.Context) []string {
			return []string{"role:support", "segment:vip"}
		}),
	)
	ctx := featureflag.WithSubject(context.Background(), "usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		if !c.Bool(ctx, "f", false) {
			b.Fatal("expected allow match")
		}
	}
}

func BenchmarkBool_Miss(b *testing.B) {
	mem := featureflag.NewMemory(nil)
	c := benchClient(b,
		featureflag.WithProvider(mem),
		featureflag.WithBool("other", true),
	)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "nope", false)
	}
}

func BenchmarkString_Coerce(b *testing.B) {
	c := benchClient(b, featureflag.WithString("s", "banner text"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.String(ctx, "s", "")
	}
}

func BenchmarkDuration_Coerce(b *testing.B) {
	c := benchClient(b, featureflag.WithDuration("d", 5*time.Second))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Duration(ctx, "d", 0)
	}
}

func BenchmarkCached_Hit(b *testing.B) {
	slow := featureflag.NewMemory(featureflag.Flags{
		"f": {Value: "true", Enabled: true, Rollout: 100},
	})
	c := benchClient(b, featureflag.WithProvider(
		featureflag.Cached(slow, time.Hour, featureflag.CacheClock(clock.System())),
	))
	ctx := context.Background()
	_ = c.Bool(ctx, "f", false) // warm
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Bool(ctx, "f", false)
	}
}

func BenchmarkCached_Hit_Parallel(b *testing.B) {
	slow := featureflag.NewMemory(featureflag.Flags{
		"f": {Value: "true", Enabled: true, Rollout: 100},
	})
	c := benchClient(b, featureflag.WithProvider(featureflag.Cached(slow, time.Hour)))
	_ = c.Bool(context.Background(), "f", false) // warm
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = c.Bool(ctx, "f", false)
		}
	})
}

func BenchmarkFor_Evaluator(b *testing.B) {
	c := benchClient(b,
		featureflag.WithBool("f", true),
		featureflag.WithRollout("f", 50),
	)
	e := c.For("usr_2a9f8c31")
	b.ReportAllocs()
	for b.Loop() {
		_ = e.Bool("f", false)
	}
}
