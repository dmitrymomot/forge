package bootstrap_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/ops/bootstrap"
	"github.com/dmitrymomot/forge/ops/buildinfo"
	"github.com/dmitrymomot/forge/ops/logger"
)

type testConfig struct {
	Port int    `env:"TEST_PORT"`
	Name string `env:"TEST_NAME"`
}

func TestRunCleanExit(t *testing.T) {
	code := bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, log *slog.Logger) error { return nil },
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
}

func TestRunErrorExitLogs(t *testing.T) {
	var buf bytes.Buffer
	code := bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error { return errors.New("boom") },
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithAutoMaxProcs(false))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("error not logged: %s", buf.String())
	}
}

func TestRunContextCancelIsCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- bootstrap.Run(ctx, "svc",
			func(runCtx context.Context, l *slog.Logger) error {
				<-runCtx.Done()
				return runCtx.Err()
			},
			bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	}()
	cancel() // supervisor.WithContext threads this like a SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0 (context.Canceled is clean)", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunWithRedactKeys(t *testing.T) {
	var buf bytes.Buffer
	bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error {
			l.Info("login", "password", "hunter2")
			return nil
		},
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithRedactKeys("password"), bootstrap.WithAutoMaxProcs(false))
	if strings.Contains(buf.String(), "hunter2") {
		t.Errorf("password not redacted: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "[REDACTED]") {
		t.Errorf("expected [REDACTED]: %s", buf.String())
	}
}

func TestRunWithBuildInfoLogs(t *testing.T) {
	var buf bytes.Buffer
	bootstrap.Run(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger) error { return nil },
		bootstrap.WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))),
		bootstrap.WithBuildInfo(buildinfo.Info{Version: "9.9.9"}),
		bootstrap.WithAutoMaxProcs(false))
	if !strings.Contains(buf.String(), "9.9.9") {
		t.Errorf("build info not logged: %s", buf.String())
	}
}

func TestRunWithConfigLoadsAndPasses(t *testing.T) {
	t.Setenv("TEST_PORT", "8080")
	t.Setenv("TEST_NAME", "svc")
	var got testConfig
	code := bootstrap.RunWithConfig(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger, cfg testConfig) error {
			got = cfg
			return nil
		},
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if got.Port != 8080 || got.Name != "svc" {
		t.Errorf("config = %+v, want {8080 svc}", got)
	}
}

func TestRunWithConfigLoadFailureSkipsFn(t *testing.T) {
	t.Setenv("TEST_PORT", "not-a-number") // typeconv int parse fails
	called := false
	code := bootstrap.RunWithConfig(context.Background(), "svc",
		func(ctx context.Context, l *slog.Logger, cfg testConfig) error {
			called = true
			return nil
		},
		bootstrap.WithLogger(logger.NewNope()), bootstrap.WithAutoMaxProcs(false))
	if code != 1 {
		t.Errorf("exit = %d, want 1 on config load failure", code)
	}
	if called {
		t.Error("fn must not be called when config load fails")
	}
}
