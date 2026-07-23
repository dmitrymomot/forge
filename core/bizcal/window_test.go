package bizcal_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/core/bizcal"
)

func TestNewWindow_Valid(t *testing.T) {
	w, err := bizcal.NewWindow(9*60, 17*60+30)
	if err != nil {
		t.Fatalf("NewWindow: unexpected error: %v", err)
	}
	if w.String() != "09:00-17:30" {
		t.Fatalf("NewWindow.String() = %s, want 09:00-17:30", w.String())
	}
}

func TestNewWindow_RejectsInverted(t *testing.T) {
	_, err := bizcal.NewWindow(17*60, 9*60)
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("NewWindow(inverted): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestNewWindow_RejectsOutOfRange(t *testing.T) {
	_, err := bizcal.NewWindow(0, 1441)
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("NewWindow(0,1441): got err=%v, want ErrInvalidWindow", err)
	}

	_, err = bizcal.NewWindow(-1, 60)
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("NewWindow(-1,60): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestNewWindow_RejectsZeroDuration(t *testing.T) {
	_, err := bizcal.NewWindow(60, 60)
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("NewWindow(60,60): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_AcceptsStandard(t *testing.T) {
	w, err := bizcal.ParseWindow("09:00-17:30")
	if err != nil {
		t.Fatalf("ParseWindow: unexpected error: %v", err)
	}
	if w.String() != "09:00-17:30" {
		t.Fatalf("ParseWindow.String() = %s, want 09:00-17:30", w.String())
	}
}

func TestParseWindow_AcceptsFullDay(t *testing.T) {
	w, err := bizcal.ParseWindow("00:00-24:00")
	if err != nil {
		t.Fatalf("ParseWindow(00:00-24:00): unexpected error: %v", err)
	}
	if w.Duration() != 24*time.Hour {
		t.Fatalf("ParseWindow(00:00-24:00).Duration() = %v, want 24h", w.Duration())
	}
}

func TestParseWindow_AcceptsSingleDigitHour(t *testing.T) {
	// Controller decision: single-digit hour is accepted (DX over strictness).
	w, err := bizcal.ParseWindow("9:00-17:00")
	if err != nil {
		t.Fatalf("ParseWindow(9:00-17:00): unexpected error: %v", err)
	}
	if w.String() != "09:00-17:00" {
		t.Fatalf("ParseWindow(9:00-17:00).String() = %s, want 09:00-17:00", w.String())
	}
}

func TestParseWindow_RejectsEmpty(t *testing.T) {
	_, err := bizcal.ParseWindow("")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindow(empty): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_RejectsInverted(t *testing.T) {
	_, err := bizcal.ParseWindow("17:00-09:00")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindow(17:00-09:00): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_RejectsMissingEnd(t *testing.T) {
	_, err := bizcal.ParseWindow("09:00")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindow(09:00): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_RejectsInvalidMinute(t *testing.T) {
	_, err := bizcal.ParseWindow("09:60-10:00")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindow(09:60-10:00): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_RejectsInvalidHour(t *testing.T) {
	_, err := bizcal.ParseWindow("09:00-25:00")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindow(09:00-25:00): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestParseWindow_RejectsGarbage(t *testing.T) {
	for _, s := range []string{"garbage", "09:00-", "-17:00", "09:00-17:00-extra", "09-17"} {
		_, err := bizcal.ParseWindow(s)
		if !errors.Is(err, bizcal.ErrInvalidWindow) {
			t.Errorf("ParseWindow(%q): got err=%v, want ErrInvalidWindow", s, err)
		}
	}
}

func TestParseWindows_Multiple(t *testing.T) {
	ws, err := bizcal.ParseWindows("09:00-12:00", "13:00-17:00")
	if err != nil {
		t.Fatalf("ParseWindows: unexpected error: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("ParseWindows: got %d windows, want 2", len(ws))
	}
	if ws[0].String() != "09:00-12:00" || ws[1].String() != "13:00-17:00" {
		t.Fatalf("ParseWindows: got %v", ws)
	}
}

func TestParseWindows_PropagatesError(t *testing.T) {
	_, err := bizcal.ParseWindows("09:00-12:00", "bad")
	if !errors.Is(err, bizcal.ErrInvalidWindow) {
		t.Fatalf("ParseWindows(bad): got err=%v, want ErrInvalidWindow", err)
	}
}

func TestMustWindows_PanicsOnGarbage(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustWindows: expected panic on garbage input")
		}
	}()
	bizcal.MustWindows("garbage")
}

func TestMustWindows_ReturnsValid(t *testing.T) {
	ws := bizcal.MustWindows("09:00-12:00", "13:00-17:00")
	if len(ws) != 2 {
		t.Fatalf("MustWindows: got %d windows, want 2", len(ws))
	}
}

func TestWindow_Duration(t *testing.T) {
	w := bizcal.MustWindows("09:00-13:00")[0]
	if w.Duration() != 4*time.Hour {
		t.Fatalf("Duration() = %v, want 4h", w.Duration())
	}
}

func TestWindow_StartEnd(t *testing.T) {
	w := bizcal.MustWindows("09:00-17:30")[0]
	sh, sm := w.Start()
	if sh != 9 || sm != 0 {
		t.Fatalf("Start() = %d:%d, want 9:0", sh, sm)
	}
	eh, em := w.End()
	if eh != 17 || em != 30 {
		t.Fatalf("End() = %d:%d, want 17:30", eh, em)
	}
}

func TestWindow_ZeroValueIsEmpty(t *testing.T) {
	var w bizcal.Window
	if w.Duration() != 0 {
		t.Fatalf("zero Window.Duration() = %v, want 0", w.Duration())
	}
}
