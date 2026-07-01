package bufpool_test

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/dmitrymomot/forge/bufpool"
)

func TestGetReturnsResetBuffer(t *testing.T) {
	b := bufpool.Get()
	if b.Len() != 0 {
		t.Fatalf("want empty, got len %d", b.Len())
	}
	b.WriteString("data")
	bufpool.Put(b)
	b2 := bufpool.Get()
	if b2.Len() != 0 {
		t.Fatalf("want reset buffer, got len %d", b2.Len())
	}
	bufpool.Put(b2)
}

func TestPutNilNoPanic(t *testing.T) {
	bufpool.Put(nil)
}

func TestDoReturnsValue(t *testing.T) {
	sentinel := errors.New("bad")
	err := bufpool.Do(func(b *bytes.Buffer) error {
		b.WriteString("hi")
		if b.String() != "hi" {
			return sentinel
		}
		return nil
	})
	if err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestDoPropagatesPanicAndPoolStaysUsable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic to propagate")
		}
		b := bufpool.Get()
		if b.Len() != 0 {
			t.Fatal("pool corrupted after panic")
		}
		bufpool.Put(b)
	}()
	_ = bufpool.Do(func(b *bytes.Buffer) error {
		panic("boom")
	})
}

func TestConcurrentRace(t *testing.T) {
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			b := bufpool.Get()
			b.WriteString("x")
			bufpool.Put(b)
		})
	}
	wg.Wait()
}
