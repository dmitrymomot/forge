package postback_test

import (
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/comms/postback"
)

func ExampleNewDestination() {
	vocab, _ := postback.NewVocabulary("click_id", "payout", "status")

	dest, _ := postback.NewDestination(
		"https://tracker.example.com/pb?cid={click_id}&sum={payout}&st={status}",
		vocab,
	)
	fmt.Println(dest.Render(map[string]string{
		"click_id": "abc123",
		"payout":   "12.50",
		"status":   "approved",
	}))

	// A typo'd macro fails at registration, never at fire time.
	_, err := postback.NewDestination("https://tracker.example.com/pb?cid={clickid}", vocab)
	fmt.Println(errors.Is(err, postback.ErrUnknownMacro))

	// Output:
	// https://tracker.example.com/pb?cid=abc123&sum=12.50&st=approved
	// true
}
