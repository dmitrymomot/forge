package clickhouse_test

import (
	"errors"
	"fmt"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

func exc(code int32) error {
	return &ch.Exception{Code: code, Message: "test", Name: "TEST"}
}

func TestCode(t *testing.T) {
	t.Parallel()
	got, ok := clickhouse.Code(exc(60))
	if !ok || got != 60 {
		t.Fatalf("Code() = (%d, %v), want (60, true)", got, ok)
	}
	// Wrapped exception is still matched.
	if _, ok := clickhouse.Code(fmt.Errorf("query: %w", exc(81))); !ok {
		t.Fatal("Code() did not unwrap")
	}
	// Non-exception and nil return false.
	if _, ok := clickhouse.Code(errors.New("plain")); ok {
		t.Fatal("Code() matched a plain error")
	}
	if _, ok := clickhouse.Code(nil); ok {
		t.Fatal("Code(nil) matched")
	}
}

func TestNamedPredicates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pred func(error) bool
		code int32
	}{
		{"IsTableNotFound", clickhouse.IsTableNotFound, 60},
		{"IsDatabaseNotFound", clickhouse.IsDatabaseNotFound, 81},
		{"IsAlreadyExists", clickhouse.IsAlreadyExists, 57},
		{"IsAuthFailed", clickhouse.IsAuthFailed, 516},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !tc.pred(exc(tc.code)) {
				t.Fatalf("%s(exc(%d)) = false, want true", tc.name, tc.code)
			}
			if tc.pred(exc(999)) {
				t.Fatalf("%s(exc(999)) = true, want false", tc.name)
			}
			if tc.pred(errors.New("plain")) {
				t.Fatalf("%s(plain) = true, want false", tc.name)
			}
		})
	}
}

func TestIsCode(t *testing.T) {
	t.Parallel()
	if !clickhouse.IsCode(exc(241), 241) {
		t.Fatal("IsCode(exc(241), 241) = false, want true")
	}
	if clickhouse.IsCode(exc(241), 60) {
		t.Fatal("IsCode(exc(241), 60) = true, want false")
	}
}
