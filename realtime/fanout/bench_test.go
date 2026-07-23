package fanout_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/dmitrymomot/forge/realtime/fanout"
)

// drain consumes a subscription as fast as possible until its channel
// closes.
func drain(sub *fanout.Subscription) {
	for range sub.C() {
	}
}

func BenchmarkPublish(b *testing.B) {
	ctx := context.Background()
	payload := []byte(`{"text":"hi"}`)

	for _, subs := range []int{1, 8, 64} {
		b.Run(strconv.Itoa(subs)+"subs", func(b *testing.B) {
			hub, err := fanout.New(fanout.WithDefaultBuffer(1024))
			if err != nil {
				b.Fatal(err)
			}
			defer hub.Close()
			for range subs {
				sub, err := hub.Subscribe(ctx, []string{"t"})
				if err != nil {
					b.Fatal(err)
				}
				go drain(sub)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := hub.Publish(ctx, "t", payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPublishMultiTopic drives concurrent publishes across several topics
// — the workload seqMu serializes to keep cross-topic delivery ordered.
func BenchmarkPublishMultiTopic(b *testing.B) {
	ctx := context.Background()
	payload := []byte(`{"text":"hi"}`)
	topics := []string{"a", "b", "c", "d"}

	hub, err := fanout.New(fanout.WithDefaultBuffer(1024))
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	for _, topic := range topics {
		sub, err := hub.Subscribe(ctx, []string{topic})
		if err != nil {
			b.Fatal(err)
		}
		go drain(sub)
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if err := hub.Publish(ctx, topics[i%len(topics)], payload); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkPublishNoSubscribers(b *testing.B) {
	ctx := context.Background()
	hub, err := fanout.New()
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	payload := []byte("x")
	b.ReportAllocs()
	for b.Loop() {
		if err := hub.Publish(ctx, "nobody", payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishReplay(b *testing.B) {
	ctx := context.Background()
	hub, err := fanout.New(fanout.WithReplay(64))
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	sub, err := hub.Subscribe(ctx, []string{"t"})
	if err != nil {
		b.Fatal(err)
	}
	go drain(sub)
	payload := []byte("x")
	b.ReportAllocs()
	for b.Loop() {
		if err := hub.Publish(ctx, "t", payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPublishScoped(b *testing.B) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "tenant-1")
	hub, err := fanout.New(fanout.WithScope(func(ctx context.Context) (string, error) {
		s, _ := ctx.Value(key{}).(string)
		return s, nil
	}))
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	sub, err := hub.Subscribe(ctx, []string{"t"})
	if err != nil {
		b.Fatal(err)
	}
	go drain(sub)
	payload := []byte("x")
	b.ReportAllocs()
	for b.Loop() {
		if err := hub.Publish(ctx, "t", payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSubscribeClose(b *testing.B) {
	ctx := context.Background()
	hub, err := fanout.New()
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	topics := []string{"t"}
	b.ReportAllocs()
	for b.Loop() {
		sub, err := hub.Subscribe(ctx, topics)
		if err != nil {
			b.Fatal(err)
		}
		sub.Close()
	}
}

func BenchmarkResume(b *testing.B) {
	ctx := context.Background()
	hub, err := fanout.New(fanout.WithReplay(64))
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	for i := range 64 {
		if err := hub.Publish(ctx, "t", []byte{byte(i)}); err != nil {
			b.Fatal(err)
		}
	}
	topics := []string{"t"}
	b.ReportAllocs()
	for b.Loop() {
		sub, err := hub.Subscribe(ctx, topics, fanout.WithResumeAfter(0), fanout.WithBuffer(64))
		if err != nil {
			b.Fatal(err)
		}
		sub.Close()
	}
}
