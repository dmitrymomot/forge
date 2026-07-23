package email_test

import (
	"bytes"
	"io"
	"testing"
	"testing/fstest"

	"github.com/dmitrymomot/forge/comms/email"
)

func BenchmarkEncodeTextOnly(b *testing.B) {
	msg := validMessage()
	for b.Loop() {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeAlternative(b *testing.B) {
	msg := validMessage()
	msg.HTML = "<p>Hi Ann,</p><p>Here is your <strong>report</strong> for July.</p>"
	for b.Loop() {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeWithAttachment(b *testing.B) {
	msg := validMessage()
	msg.HTML = "<p>Report attached.</p>"
	msg.Attachments = []email.Attachment{{Filename: "report.pdf", Content: bytes.Repeat([]byte("forge"), 64<<10/5)}}
	for b.Loop() {
		if err := msg.Encode(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTemplatesRender(b *testing.B) {
	fsys := fstest.MapFS{"welcome.tmpl": {Data: []byte(
		`{{define "welcome:subject"}}Welcome, {{.Name}}!{{end}}` +
			`{{define "welcome:html"}}<p>Hi {{.Name}},</p><p>Glad to have you.</p>{{end}}` +
			`{{define "welcome:text"}}Hi {{.Name}}, glad to have you.{{end}}`,
	)}}
	tpl, err := email.ParseFS(fsys, "*.tmpl")
	if err != nil {
		b.Fatal(err)
	}
	data := map[string]string{"Name": "Ann"}
	for b.Loop() {
		if _, err := tpl.Render("welcome", data); err != nil {
			b.Fatal(err)
		}
	}
}
