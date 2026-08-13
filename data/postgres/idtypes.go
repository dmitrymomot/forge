package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/core/id"
)

// RegisterIDTypes teaches m to bind core/id identifiers without the canonical-string
// detour.
//
// id.UUID implements driver.Valuer, and pgx skips its underlying-type lookup for any
// driver.Valuer, so an unregistered id.UUID reaches the wire only by formatting the
// 36-character string and parsing it back into 16 bytes. The wrap plan registered here
// hands pgx the underlying [16]byte instead, which its uuid codec appends directly:
// 825ns/20 allocs down to 47ns/3 allocs per bind. Slices ride pgx's own array
// wrappers, so a uuid[] column takes the same path.
//
// Scanning already resolves to the binary form without help, so no scan plan is
// registered. Registering twice is harmless.
//
// Open installs this on every pooled connection, so a consumer only calls it directly
// for a *pgx.Conn it built itself:
//
//	conn, err := pgx.Connect(ctx, url)
//	postgres.RegisterIDTypes(conn.TypeMap())
func RegisterIDTypes(m *pgtype.Map) {
	m.TryWrapEncodePlanFuncs = append(
		[]pgtype.TryWrapEncodePlanFunc{tryWrapIDEncodePlan},
		m.TryWrapEncodePlanFuncs...,
	)
}

// afterConnectHook is pgxpool's per-connection callback.
type afterConnectHook = func(context.Context, *pgx.Conn) error

func registerIDTypesHook(_ context.Context, conn *pgx.Conn) error {
	if conn == nil {
		return nil
	}
	RegisterIDTypes(conn.TypeMap())
	return nil
}

// chainAfterConnect returns a hook running first and then prev. A nil prev yields
// first alone; a failing first skips prev.
func chainAfterConnect(first, prev afterConnectHook) afterConnectHook {
	if prev == nil {
		return first
	}
	return func(ctx context.Context, conn *pgx.Conn) error {
		if err := first(ctx, conn); err != nil {
			return err
		}
		return prev(ctx, conn)
	}
}

// chainIDTypeRegistration installs RegisterIDTypes on every new connection, keeping
// whatever AfterConnect hook is already set. It runs after the WithPoolConfig escape
// hatch, whose documented purpose includes setting an AfterConnect hook: assigning
// the field there would otherwise drop the registration and silently return every
// id.UUID bind to the canonical-string path.
func chainIDTypeRegistration(poolCfg *pgxpool.Config) {
	poolCfg.AfterConnect = chainAfterConnect(registerIDTypesHook, poolCfg.AfterConnect)
}

// tryWrapIDEncodePlan converts an id.UUID value to the [16]byte pgx's uuid codec
// encodes natively. It must run before pgx's own wrappers, which bail out on any
// driver.Valuer. An invalid id.NullUUID is left alone so it still binds as SQL NULL
// through driver.Valuer.
func tryWrapIDEncodePlan(value any) (pgtype.WrappedEncodePlanNextSetter, any, bool) {
	switch v := value.(type) {
	case id.UUID:
		return &idEncodePlan{}, [16]byte(v), true
	case *id.UUID:
		if v == nil {
			return nil, nil, false
		}
		return &idEncodePlan{}, [16]byte(*v), true
	case id.NullUUID:
		if !v.Valid {
			return nil, nil, false
		}
		return &idEncodePlan{}, [16]byte(v.UUID), true
	default:
		return nil, nil, false
	}
}

type idEncodePlan struct {
	next pgtype.EncodePlan
}

func (p *idEncodePlan) SetNext(next pgtype.EncodePlan) { p.next = next }

func (p *idEncodePlan) Encode(value any, buf []byte) ([]byte, error) {
	switch v := value.(type) {
	case id.UUID:
		return p.next.Encode([16]byte(v), buf)
	case *id.UUID:
		if v == nil {
			return p.next.Encode(value, buf)
		}
		return p.next.Encode([16]byte(*v), buf)
	case id.NullUUID:
		return p.next.Encode([16]byte(v.UUID), buf)
	default:
		return p.next.Encode(value, buf)
	}
}
