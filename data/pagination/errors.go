package pagination

import "errors"

var (
	// ErrBadCursor is returned by Codec.Decode when a cursor cannot be
	// decoded, or when its signature fails to verify. The two cases are
	// deliberately indistinguishable so a caller cannot probe the boundary.
	ErrBadCursor = errors.New("pagination: bad cursor")

	// ErrEmptyKeyset is returned by Keyset.Where and Keyset.OrderBy when the
	// keyset has no columns: a keyset must name at least one column, the last
	// of which is unique.
	ErrEmptyKeyset = errors.New("pagination: empty keyset")

	// ErrInvalidColumn is returned when a keyset column is not a plain,
	// optionally qualified SQL identifier, so a misuse cannot smuggle SQL
	// into the emitted fragment.
	ErrInvalidColumn = errors.New("pagination: invalid column identifier")

	// ErrCursorArity is returned by Keyset.Where when the cursor's value
	// count does not match the keyset's column count — typically a cursor
	// minted for a different ordering.
	ErrCursorArity = errors.New("pagination: cursor arity mismatch")

	// ErrInvalidStart is returned by Keyset.Where when the Dollar dialect is
	// given a placeholder start index below 1.
	ErrInvalidStart = errors.New("pagination: invalid placeholder start index")

	// ErrInvalidDialect is returned by Keyset.Where when the dialect is
	// neither Dollar nor Question.
	ErrInvalidDialect = errors.New("pagination: invalid dialect")

	// ErrNilSigner is returned by NewCodec when WithSigner is given a nil
	// signer.
	ErrNilSigner = errors.New("pagination: nil signer")

	// ErrInvalidSize is returned by NewPage when the page size is below 1 —
	// a page must hold at least one row. Guarding it fails closed instead of
	// panicking on a slice bound when size comes from an unvalidated request.
	ErrInvalidSize = errors.New("pagination: invalid page size")
)
