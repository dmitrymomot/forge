//go:build integration

package redisbus_test

import (
	"context"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/dmitrymomot/forge/realtime/fanout/redisbus"
	"github.com/dmitrymomot/forge/testkit/redistest"
)

func BenchmarkPublish(b *testing.B) {
	client := goredis.NewClient(&goredis.Options{Addr: redistest.Addr(b)})
	defer func() { _ = client.Close() }()
	bus, err := redisbus.New(client, redisbus.WithChannel("fanout:bench"))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	payload := []byte(`{"text":"hi"}`)
	b.ReportAllocs()
	for b.Loop() {
		if err := bus.Publish(ctx, "bench", payload); err != nil {
			b.Fatal(err)
		}
	}
}
