package clickhouse

import (
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func TestBuildOptions_LZ4DefaultWhenDSNSilent(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression == nil {
		t.Fatal("Compression = nil, want LZ4 default")
	}
	if opts.Compression.Method != ch.CompressionLZ4 {
		t.Fatalf("Compression.Method = %v, want LZ4", opts.Compression.Method)
	}
}

func TestBuildOptions_DSNCompressionWins(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db?compress=zstd"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression == nil || opts.Compression.Method != ch.CompressionZSTD {
		t.Fatalf("Compression = %+v, want ZSTD from DSN", opts.Compression)
	}
}

func TestBuildOptions_CompressFalseStaysOff(t *testing.T) {
	t.Parallel()
	// compress=false must NOT be overridden by the LZ4 default. ParseDSN leaves
	// Compression nil for compress=false, so the default must gate on the raw DSN.
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db?compress=false"
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.Compression != nil {
		t.Fatalf("Compression = %+v, want nil (caller disabled it)", opts.Compression)
	}
}

func TestBuildOptions_Overlay(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://localhost:9000/db"
	cfg.MaxOpenConns = 42
	cfg.MaxIdleConns = 7
	opts, err := buildOptions(cfg)
	if err != nil {
		t.Fatalf("buildOptions() error = %v", err)
	}
	if opts.MaxOpenConns != 42 || opts.MaxIdleConns != 7 {
		t.Fatalf("overlay = (%d,%d), want (42,7)", opts.MaxOpenConns, opts.MaxIdleConns)
	}
	if opts.DialTimeout != cfg.DialTimeout {
		t.Fatalf("DialTimeout = %v, want %v", opts.DialTimeout, cfg.DialTimeout)
	}
}

func TestBuildOptions_ParseError(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.DSN = "clickhouse://" // empty host -> ParseDSN fails
	if _, err := buildOptions(cfg); err == nil {
		t.Fatal("buildOptions() error = nil, want ErrConnect")
	}
}

func TestDSNHasParam(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dsn, key string
		want     bool
	}{
		{"clickhouse://h:9000/db?compress=lz4", "compress", true},
		{"clickhouse://h:9000/db?compress=false", "compress", true},
		{"clickhouse://h:9000/db?compress_level=3", "compress", false},
		{"clickhouse://h:9000/db", "compress", false},
		{"clickhouse://h1:9000,h2:9000/db?compress=zstd", "compress", true}, // multi-host authority
	}
	for _, c := range cases {
		if got := dsnHasParam(c.dsn, c.key); got != c.want {
			t.Errorf("dsnHasParam(%q, %q) = %v, want %v", c.dsn, c.key, got, c.want)
		}
	}
}
