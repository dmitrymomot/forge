package collector_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/async/collector"
)

type click struct {
	path   string
	tenant string
}

func benchCollector(b *testing.B, opts ...collector.Option) (*collector.Collector[click], func()) {
	b.Helper()
	sink := collector.SinkFunc[click](func(context.Context, []click) error { return nil })
	opts = append([]collector.Option{collector.WithConfig(collector.Config{BufferSize: 1 << 16, BatchSize: 4096, FlushInterval: time.Millisecond, FlushTimeout: time.Second})}, opts...)
	c, err := collector.New(sink, opts...)
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = c.Run(ctx) }()
	return c, func() {
		cancel()
		<-done
	}
}

func BenchmarkAdd(b *testing.B) {
	c, stop := benchCollector(b)
	defer stop()
	ctx := context.Background()
	ev := click{path: "/pricing"}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Add(ctx, ev)
	}
}

func BenchmarkAddParallel(b *testing.B) {
	c, stop := benchCollector(b)
	defer stop()
	ev := click{path: "/pricing"}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_ = c.Add(ctx, ev)
		}
	})
}

type benchTenantKey struct{}

func BenchmarkAddScoped(b *testing.B) {
	c, stop := benchCollector(b,
		collector.WithScope(func(ctx context.Context) (string, error) {
			v, _ := ctx.Value(benchTenantKey{}).(string)
			return v, nil
		}),
		collector.WithScopeContext(func(ctx context.Context, scope string) context.Context {
			return context.WithValue(ctx, benchTenantKey{}, scope)
		}),
	)
	defer stop()
	ctx := context.WithValue(context.Background(), benchTenantKey{}, "tenant-a")
	ev := click{path: "/pricing", tenant: "a"}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.Add(ctx, ev)
	}
}
