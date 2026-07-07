package supervisor_test

import (
	"context"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/supervisor"
)

func TestWithContextParentCancels(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop := supervisor.NewContext(supervisor.WithContext(parent))
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("ctx cancelled before parent")
	default:
	}
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ctx not cancelled after parent cancel")
	}
}

func TestWithContextParentCancelsForceQuit(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, stop := supervisor.NewContext(supervisor.WithContext(parent), supervisor.WithForceQuit())
	defer stop()
	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("force-quit ctx not cancelled after parent cancel")
	}
}

func TestNewContextDefaultsToBackground(t *testing.T) {
	ctx, stop := supervisor.NewContext()
	defer stop()
	select {
	case <-ctx.Done():
		t.Fatal("default context should not be cancelled")
	default:
	}
}
