package session

import (
	"encoding/json"
	"testing"
)

func TestEncodePreservesUnknownRawKeys(t *testing.T) {
	rec := Record{Payload: []byte(`{"known":{"a":1},"unknown":{"b":2}}`)}
	s := newSession(rec, "tok", false, nil)

	s.markDirty("known", map[string]int{"a": 9})
	if err := s.encode(); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(s.record().Payload, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(out["unknown"]) != `{"b":2}` {
		t.Fatalf("unknown key mutated: %s", out["unknown"])
	}
	if string(out["known"]) != `{"a":9}` {
		t.Fatalf("known key not updated: %s", out["known"])
	}
}

// namespaceDrift is a local stand-in for prefsData: staging a schema-drift
// decode failure means writing one type under a name and reading a different
// type from it, which a black-box test cannot express through NewNamespace's
// single type parameter. Constructing the Namespace struct literal here
// reproduces the drift directly.
type namespaceDrift struct {
	Theme string `json:"theme"`
}

func TestNamespaceDecodeFailureIsAnError(t *testing.T) {
	s := newSession(Record{Payload: []byte(`{"drifted":[1,2]}`)}, "tok", false, nil)
	ns := &Namespace[namespaceDrift]{name: "drifted"}
	if _, err := ns.Get(s); err == nil {
		t.Fatal("Get must return an error on a decode failure, never a zero value")
	}
}
