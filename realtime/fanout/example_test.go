package fanout_test

import (
	"context"
	"fmt"

	"github.com/dmitrymomot/forge/realtime/fanout"
)

func Example() {
	ctx := context.Background()

	hub, err := fanout.New(fanout.WithReplay(16))
	if err != nil {
		panic(err)
	}
	defer hub.Close()

	sub, err := hub.Subscribe(ctx, []string{"chat.42"})
	if err != nil {
		panic(err)
	}
	defer sub.Close()

	if err := hub.Publish(ctx, "chat.42", []byte("hello")); err != nil {
		panic(err)
	}

	msg := <-sub.C()
	fmt.Println(msg.Topic, string(msg.Payload))

	// A reconnecting client resumes from the replay ring with no gap.
	resumed, err := hub.Subscribe(ctx, []string{"chat.42"}, fanout.WithResumeAfter(0))
	if err != nil {
		panic(err)
	}
	defer resumed.Close()
	replay := <-resumed.C()
	fmt.Println(replay.ID == msg.ID)

	// Output:
	// chat.42 hello
	// true
}
