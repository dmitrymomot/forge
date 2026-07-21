package approval_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/approval"
)

func FuzzPayloadOf(f *testing.F) {
	f.Add([]byte(`{"payout_id":"po_1","amount":100}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Add([]byte(`{"amount":"not-a-number"}`))
	f.Add([]byte(`[1,2,3]`))
	f.Add([]byte("\x00\x01\x02"))

	f.Fuzz(func(t *testing.T, payload []byte) {
		r := approval.Request{
			ID:      id.NewUUID(),
			Kind:    kindPayout.Name(),
			Payload: json.RawMessage(payload),
		}
		// Must never panic; an error is a perfectly good outcome.
		_, _ = approval.PayloadOf(kindPayout, r)
	})
}
