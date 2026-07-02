package null_test

import (
	"encoding/json"
	"testing"

	"github.com/dmitrymomot/forge/null"
)

func TestOfEmptyGet(t *testing.T) {
	if v, ok := null.Of("hi").Get(); !ok || v != "hi" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
	if _, ok := null.Empty[string]().Get(); ok {
		t.Fatal("want invalid")
	}
}

func TestPtrAndFromPtr(t *testing.T) {
	if null.Empty[int]().Ptr() != nil {
		t.Fatal("want nil Ptr")
	}
	orig := null.Of(9)
	p := orig.Ptr()
	if p == nil || *p != 9 {
		t.Fatalf("got %v", p)
	}
	*p = 100 // mutating the returned pointer must not affect the source Null
	if v, ok := orig.Get(); !ok || v != 9 {
		t.Fatalf("Ptr() aliased internal storage: orig became %d (ok=%v)", v, ok)
	}

	x := 3
	if v, ok := null.FromPtr(&x).Get(); !ok || v != 3 {
		t.Fatalf("got %d ok=%v", v, ok)
	}
	if _, ok := null.FromPtr[int](nil).Get(); ok {
		t.Fatal("want invalid for nil pointer")
	}
}

func TestJSON(t *testing.T) {
	b, err := json.Marshal(null.Of(5))
	if err != nil || string(b) != "5" {
		t.Fatalf("got %s, %v", b, err)
	}
	b, err = json.Marshal(null.Empty[int]())
	if err != nil || string(b) != "null" {
		t.Fatalf("got %s, %v", b, err)
	}

	var n null.Null[int]
	if err := json.Unmarshal([]byte("null"), &n); err != nil {
		t.Fatal(err)
	}
	if _, ok := n.Get(); ok {
		t.Fatal("want invalid after null")
	}
	if err := json.Unmarshal([]byte("7"), &n); err != nil {
		t.Fatal(err)
	}
	if v, ok := n.Get(); !ok || v != 7 {
		t.Fatalf("got %d ok=%v", v, ok)
	}
}

func TestSQLRoundTrip(t *testing.T) {
	n := null.Of[string]("hi")
	v, err := n.Value()
	if err != nil {
		t.Fatal(err)
	}
	var m null.Null[string]
	if err := m.Scan(v); err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Get(); !ok || got != "hi" {
		t.Fatalf("got %q ok=%v", got, ok)
	}

	e := null.Empty[int64]()
	ev, err := e.Value()
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Fatalf("want nil driver value, got %v", ev)
	}
	var me null.Null[int64]
	if err := me.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := me.Get(); ok {
		t.Fatal("want invalid after Scan(nil)")
	}
}
