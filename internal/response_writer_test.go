package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	rw.WriteHeader(http.StatusNotFound)

	if rw.Status() != http.StatusNotFound {
		t.Errorf("Status() = %d, want %d", rw.Status(), http.StatusNotFound)
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("underlying status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if !rw.Written() {
		t.Error("Written() = false, want true")
	}
}

func TestResponseWriter_WriteHeader_HTMX(t *testing.T) {
	tests := []struct {
		name           string
		inputCode      int
		expectedCode   int
		expectedStatus int
	}{
		{"200 stays 200", http.StatusOK, http.StatusOK, http.StatusOK},
		{"400 becomes 200", http.StatusBadRequest, http.StatusOK, http.StatusBadRequest},
		{"404 becomes 200", http.StatusNotFound, http.StatusOK, http.StatusNotFound},
		{"500 becomes 200", http.StatusInternalServerError, http.StatusOK, http.StatusInternalServerError},
		{"302 becomes 200", http.StatusFound, http.StatusOK, http.StatusFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			rw := NewResponseWriter(w, true) // HTMX request

			rw.WriteHeader(tt.inputCode)

			// Status() returns the original code (for logging/debugging)
			if rw.Status() != tt.expectedStatus {
				t.Errorf("Status() = %d, want %d", rw.Status(), tt.expectedStatus)
			}
			// Underlying writer gets transformed code for HTMX
			if w.Code != tt.expectedCode {
				t.Errorf("underlying status = %d, want %d", w.Code, tt.expectedCode)
			}
		})
	}
}

func TestResponseWriter_WriteHeader_OnlyOnce(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	rw.WriteHeader(http.StatusOK)
	rw.WriteHeader(http.StatusNotFound) // Should be ignored

	if rw.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", rw.Status(), http.StatusOK)
	}
	if w.Code != http.StatusOK {
		t.Errorf("underlying status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestResponseWriter_Write(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	data := []byte("hello world")
	n, err := rw.Write(data)

	if err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() = %d, want %d", n, len(data))
	}
	if rw.Size() != int64(len(data)) {
		t.Errorf("Size() = %d, want %d", rw.Size(), len(data))
	}
	if !rw.Written() {
		t.Error("Written() = false, want true")
	}
	if w.Body.String() != "hello world" {
		t.Errorf("body = %q, want %q", w.Body.String(), "hello world")
	}
}

func TestResponseWriter_OnBeforeWrite(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	var hookCalled bool
	rw.OnBeforeWrite(func() {
		hookCalled = true
	})

	rw.WriteHeader(http.StatusOK)

	if !hookCalled {
		t.Error("hook was not called")
	}
}

func TestResponseWriter_OnBeforeWrite_MultipleHooks(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	var order []int
	rw.OnBeforeWrite(func() { order = append(order, 1) })
	rw.OnBeforeWrite(func() { order = append(order, 2) })
	rw.OnBeforeWrite(func() { order = append(order, 3) })

	rw.WriteHeader(http.StatusOK)

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks called, got %d", len(order))
	}
	for i, v := range order {
		if v != i+1 {
			t.Errorf("hook order[%d] = %d, want %d", i, v, i+1)
		}
	}
}

func TestResponseWriter_OnBeforeWrite_CalledOnce(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	callCount := 0
	rw.OnBeforeWrite(func() { callCount++ })

	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte("data"))

	if callCount != 1 {
		t.Errorf("hook called %d times, want 1", callCount)
	}
}

func TestResponseWriter_OnBeforeWrite_CalledOnFirstWrite(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	var hookCalled bool
	rw.OnBeforeWrite(func() { hookCalled = true })

	rw.Write([]byte("data")) // Write without explicit WriteHeader

	if !hookCalled {
		t.Error("hook was not called on Write")
	}
}

func TestResponseWriter_Flush(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	// Should not panic
	rw.Flush()

	if !w.Flushed {
		t.Error("underlying flusher not called")
	}
}

func TestResponseWriter_Unwrap(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	if rw.Unwrap() != w {
		t.Error("Unwrap() did not return underlying writer")
	}
}

func TestResponseWriter_Header(t *testing.T) {
	w := httptest.NewRecorder()
	rw := NewResponseWriter(w, false)

	rw.Header().Set("X-Test", "value")

	if got := w.Header().Get("X-Test"); got != "value" {
		t.Errorf("Header X-Test = %q, want %q", got, "value")
	}
}
