package session_test

import (
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/auth/session"
)

type cartData struct {
	Items []string `json:"items"`
}

type prefsData struct {
	Theme string `json:"theme"`
}

var (
	nsCart  = session.NewNamespace[cartData]("test.cart")
	nsPrefs = session.NewNamespace[prefsData]("test.prefs")
)

func TestNamespaceRoundTrip(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	got, err := nsCart.Get(sess)
	if err != nil {
		t.Fatalf("Get on empty session: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("empty session returned %v, want zero value", got)
	}

	nsCart.Set(sess, cartData{Items: []string{"sku-1"}})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err = nsCart.Get(reloaded)
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0] != "sku-1" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestUnknownNamespaceSurvivesSave(t *testing.T) {
	mgr := newTestManager(t)
	sess := mgr.Start()

	nsCart.Set(sess, cartData{Items: []string{"sku-1"}})
	nsPrefs.Set(sess, prefsData{Theme: "dark"})
	if err := mgr.Save(t.Context(), sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A second process touches only "cart" and never mentions "prefs".
	reloaded, err := mgr.Load(t.Context(), sess.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	nsCart.Set(reloaded, cartData{Items: []string{"sku-2"}})
	if err := mgr.Save(t.Context(), reloaded); err != nil {
		t.Fatalf("Save: %v", err)
	}

	final, err := mgr.Load(t.Context(), reloaded.Token())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prefs, err := nsPrefs.Get(final)
	if err != nil {
		t.Fatalf("Get prefs: %v", err)
	}
	if prefs.Theme != "dark" {
		t.Fatalf("untouched namespace was dropped: %+v", prefs)
	}
}

func TestDuplicateNamespacePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering a duplicate namespace name must panic at init")
		}
		if !strings.Contains(toString(r), "test.cart") {
			t.Fatalf("panic message %v must name the colliding namespace", r)
		}
	}()
	_ = session.NewNamespace[cartData]("test.cart")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
