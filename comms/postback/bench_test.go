package postback_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/comms/postback"
)

func benchVocab(b *testing.B) postback.Vocabulary {
	b.Helper()
	v, err := postback.NewVocabulary("click_id", "payout", "status", "sub1", "sub2")
	if err != nil {
		b.Fatal(err)
	}
	return v
}

func BenchmarkNewDestination(b *testing.B) {
	vocab := benchVocab(b)
	raw := "https://tracker.example.com/pb?cid={click_id}&sum={payout}&st={status}&s1={sub1}&s2={sub2}"
	b.ReportAllocs()
	for b.Loop() {
		if _, err := postback.NewDestination(raw, vocab); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRender(b *testing.B) {
	vocab := benchVocab(b)
	dest, err := postback.NewDestination(
		"https://tracker.example.com/pb?cid={click_id}&sum={payout}&st={status}&s1={sub1}&s2={sub2}",
		vocab,
	)
	if err != nil {
		b.Fatal(err)
	}
	values := map[string]string{
		"click_id": "8f14e45fceea167a",
		"payout":   "12.50",
		"status":   "approved",
		"sub1":     "campaign-7",
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = dest.Render(values)
	}
}

func BenchmarkRenderPathMacro(b *testing.B) {
	vocab := benchVocab(b)
	dest, err := postback.NewDestination("https://tracker.example.com/pb/{click_id}/{status}?s1={sub1}", vocab)
	if err != nil {
		b.Fatal(err)
	}
	values := map[string]string{
		"click_id": "8f14e45fceea167a",
		"status":   "approved",
		"sub1":     "campaign-7",
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = dest.Render(values)
	}
}

func BenchmarkSend(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	b.Cleanup(srv.Close)
	sender := postback.New(postback.WithHTTPClient(srv.Client()))
	vocab := benchVocab(b)
	dest, err := postback.NewDestination(srv.URL+"/pb?cid={click_id}&sum={payout}", vocab)
	if err != nil {
		b.Fatal(err)
	}
	values := map[string]string{"click_id": "8f14e45fceea167a", "payout": "12.50"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sender.Send(b.Context(), dest, values); err != nil {
			b.Fatal(err)
		}
	}
}
