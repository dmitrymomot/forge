package clickhouse_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/dmitrymomot/forge/data/clickhouse"
)

// fakeCloser records whether Close was called and can return a forced error.
type fakeCloser struct {
	closed bool
	err    error
}

func (f *fakeCloser) Close() error {
	f.closed = true
	return f.err
}

func TestClose_NilTolerated(t *testing.T) {
	t.Parallel()
	// Must not panic with a nil closer and/or nil logger.
	clickhouse.Close(nil, nil)
}

func TestClose_ClosesCloser(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{}
	clickhouse.Close(fc, nil)
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

func TestClose_CloseErrorTolerated(t *testing.T) {
	t.Parallel()
	fc := &fakeCloser{err: errors.New("boom")}
	clickhouse.Close(fc, nil) // must not panic even though Close errors
	if !fc.closed {
		t.Fatal("Close did not call the underlying Close")
	}
}

// fakeCHConn is a native clickhouse.Conn whose Ping returns a forced error,
// letting the Healthcheck wrapping be exercised without a live server. Only Ping
// is called by Healthcheck; the embedded nil Conn covers the rest of the interface.
type fakeCHConn struct {
	ch.Conn
	pingErr error
}

func (f fakeCHConn) Ping(context.Context) error { return f.pingErr }

func TestHealthcheck_Success(t *testing.T) {
	t.Parallel()
	if err := clickhouse.Healthcheck(fakeCHConn{})(context.Background()); err != nil {
		t.Fatalf("Healthcheck() = %v, want nil", err)
	}
}

func TestHealthcheck_WrapsError(t *testing.T) {
	t.Parallel()
	err := clickhouse.Healthcheck(fakeCHConn{pingErr: errors.New("boom")})(context.Background())
	if !errors.Is(err, clickhouse.ErrHealthcheck) {
		t.Fatalf("Healthcheck() = %v, want ErrHealthcheck", err)
	}
}

// fakeConnector builds a *sql.DB whose PingContext outcome is controlled by pingErr,
// exercising HealthcheckDB's wrapping without a live server.
type fakeConnector struct{ pingErr error }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeDriverConn(c), nil
}
func (fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type fakeDriverConn struct{ pingErr error }

func (c fakeDriverConn) Ping(context.Context) error        { return c.pingErr }
func (fakeDriverConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (fakeDriverConn) Close() error                        { return nil }
func (fakeDriverConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }

func TestHealthcheckDB_Success(t *testing.T) {
	t.Parallel()
	db := sql.OpenDB(fakeConnector{})
	defer func() { _ = db.Close() }()
	if err := clickhouse.HealthcheckDB(db)(context.Background()); err != nil {
		t.Fatalf("HealthcheckDB() = %v, want nil", err)
	}
}

func TestHealthcheckDB_WrapsError(t *testing.T) {
	t.Parallel()
	db := sql.OpenDB(fakeConnector{pingErr: errors.New("boom")})
	defer func() { _ = db.Close() }()
	err := clickhouse.HealthcheckDB(db)(context.Background())
	if !errors.Is(err, clickhouse.ErrHealthcheck) {
		t.Fatalf("HealthcheckDB() = %v, want ErrHealthcheck", err)
	}
}
