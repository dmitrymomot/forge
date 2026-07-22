package email_test

import (
	"context"
	"fmt"
	"testing/fstest"
	"time"

	"github.com/dmitrymomot/forge/comms/email"
)

// Example wires the full flow: config, template rendering, and an SMTP send.
func Example() {
	sender, err := email.New(email.Config{
		Addr:     "smtp.example.com:587",
		Username: "postmaster",
		Password: "secret",
		TLS:      email.TLSStartTLS,
		From:     "Acme <no-reply@acme.example>",
		Timeout:  15 * time.Second,
	})
	if err != nil {
		panic(err)
	}

	msg := email.Message{
		To:      []string{"Ann <ann@example.com>"},
		Subject: "Welcome to Acme",
		Text:    "Hi Ann, welcome aboard!",
		HTML:    "<p>Hi Ann, welcome aboard!</p>",
	}
	_ = sender.Send(context.Background(), msg)
}

func ExampleTemplates_Render() {
	fsys := fstest.MapFS{"welcome.tmpl": {Data: []byte(
		`{{define "welcome:subject"}}Welcome, {{.Name}}!{{end}}` +
			`{{define "welcome:html"}}<p>Hi {{.Name}},</p>{{end}}` +
			`{{define "welcome:text"}}Hi {{.Name}},{{end}}`,
	)}}
	tpl, err := email.ParseFS(fsys, "*.tmpl")
	if err != nil {
		panic(err)
	}
	msg, err := tpl.Render("welcome", map[string]string{"Name": "Ann"})
	if err != nil {
		panic(err)
	}
	fmt.Println(msg.Subject)
	fmt.Println(msg.HTML)
	fmt.Println(msg.Text)
	// Output:
	// Welcome, Ann!
	// <p>Hi Ann,</p>
	// Hi Ann,
}
