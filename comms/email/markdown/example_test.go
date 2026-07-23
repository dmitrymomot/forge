package markdown_test

import (
	"fmt"

	"github.com/dmitrymomot/forge/comms/email/markdown"
)

func ExampleRenderer_Render() {
	r, err := markdown.New()
	if err != nil {
		panic(err)
	}
	msg, err := r.Render([]byte(`---
subject: Confirm your email
preheader: One click and you're in.
---
# Almost there

Confirm your address to activate your account.

[Button: Confirm email](https://app.acme.example/confirm?t=abc)
`))
	if err != nil {
		panic(err)
	}
	fmt.Println(msg.Subject)
	fmt.Println(msg.Text)
	// Output:
	// Confirm your email
	// Almost there
	//
	// Confirm your address to activate your account.
	//
	// Confirm email: https://app.acme.example/confirm?t=abc
}
