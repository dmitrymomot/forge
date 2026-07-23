package markdown_test

import (
	"testing"

	"github.com/dmitrymomot/forge/comms/email/markdown"
)

func BenchmarkRender(b *testing.B) {
	r, err := markdown.New()
	if err != nil {
		b.Fatal(err)
	}
	src := []byte(welcomeDoc)
	for b.Loop() {
		if _, err := r.Render(src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderData(b *testing.B) {
	r, err := markdown.New()
	if err != nil {
		b.Fatal(err)
	}
	src := []byte("---\nsubject: Welcome, {{.Name}}!\n---\n# Hi {{.Name}}\n\n[Button: Start](https://app.acme.example/start?t={{.Token}})\n")
	data := map[string]string{"Name": "Ann", "Token": "abc123"}
	for b.Loop() {
		if _, err := r.RenderData(src, data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderLongDocument(b *testing.B) {
	r, err := markdown.New()
	if err != nil {
		b.Fatal(err)
	}
	src := []byte("---\nsubject: Digest\npreheader: Your weekly digest\n---\n")
	for range 30 {
		src = append(src, []byte("## Section\n\nSome paragraph with **bold** and a [link](https://acme.example/x).\n\n- item one\n- item two\n- item three\n\n")...)
	}
	src = append(src, []byte("[Button: Open dashboard](https://app.acme.example)\n")...)
	for b.Loop() {
		if _, err := r.Render(src); err != nil {
			b.Fatal(err)
		}
	}
}
