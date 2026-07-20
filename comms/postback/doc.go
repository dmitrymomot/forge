// Package postback fires tracker-style server-to-server postbacks — the
// affiliate/ad-network conversion ping: an unsigned GET (or POST) to a
// partner-configured URL template whose {macro} placeholders carry the click
// ID, payout, status, and sub-IDs.
//
// The macro vocabulary is consumer data: register the names your platform
// exposes with NewVocabulary, then parse each partner URL with
// NewDestination. Validation is fail-closed at registration — an unknown
// macro, unbalanced braces, a non-http(s) or relative URL, a fragment, or a
// macro in the authority are construction errors, never an empty
// substitution at fire time. A registered macro absent from the per-event
// value map renders as an empty string: sub-IDs are genuinely sparse and
// trackers accept empty parameters. Values are URL-escaped for their
// position (query vs path), so a hostile value can't alter the URL structure
// validated at registration.
//
// Send renders, fires through one reused *http.Client, and reports the
// outcome by status class: nil for 2xx, ErrServerStatus for 5xx (transient —
// worth retrying), ErrClientStatus for everything else (permanent — the
// destination or event is wrong). That split maps directly onto a queue's
// retry-vs-dead-letter decision.
//
// # Non-goals
//
//   - No signatures: trackers correlate by click ID, not HMAC — signed
//     deliveries are comms/webhook.
//   - No dedup: stable event IDs ride as macros; receivers dedup.
//   - No durable delivery or per-destination fan-out: ride async/queue /
//     async/eventrouter, where Send slots in as a deliverer.
//   - No per-tracker format tables: which macros exist and which template
//     each partner registered is consumer data.
//   - No tenant seam: a Destination is a passed value; scope destinations in
//     the consumer store that holds them.
//
// # Usage
//
//	vocab, _ := postback.NewVocabulary("click_id", "payout", "status", "sub1")
//	dest, err := postback.NewDestination(
//		"https://tracker.example.com/pb?cid={click_id}&sum={payout}&st={status}",
//		vocab,
//	)
//	if err != nil {
//		// reject the partner's URL at registration time
//	}
//
//	sender := postback.New()
//	res, err := sender.Send(ctx, dest, map[string]string{
//		"click_id": "abc123",
//		"payout":   "12.50",
//		"status":   "approved",
//	})
//	// res.URL and res.StatusCode feed the audit log;
//	// errors.Is(err, postback.ErrServerStatus) → requeue.
package postback
