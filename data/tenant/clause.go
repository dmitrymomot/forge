package tenant

import "context"

// Clause is a parameterized SQL fragment scoping a query to the current
// tenant. The consumer concatenates SQL into the query text and passes Arg
// as the matching argument — the scope stays visible at every query and is
// never auto-injected:
//
//	c, err := tenant.ScopeClause(ctx, "tenant_id", "$2")
//	if err != nil {
//		return err
//	}
//	rows, err := db.Query(ctx,
//		"SELECT id FROM orders WHERE status = $1 AND "+c.SQL, status, c.Arg)
type Clause struct {
	// SQL is the fragment, e.g. `tenant_id = $2` or `tenant_id = ?`.
	SQL string
	// Arg is the tenant ID to bind for the placeholder.
	Arg string
}

// ScopeClause builds the tenant-scoping fragment `column = placeholder` for
// the tenant in ctx. placeholder is written into the SQL verbatim, so pass
// exactly what the query needs at that position: "$n" for pgx/postgres
// ("$2" above) or "?" for sqlite/mysql/clickhouse.
//
// Fail-closed: ErrNoTenant when ctx carries no tenant. column must be a
// plain, optionally qualified identifier (`orders.tenant_id`) and
// placeholder must be "?" or "$n" — anything else is rejected with
// ErrInvalidColumn / ErrInvalidPlaceholder so a misuse cannot smuggle SQL
// into the fragment.
func ScopeClause(ctx context.Context, column, placeholder string) (Clause, error) {
	if !validColumn(column) {
		return Clause{}, ErrInvalidColumn
	}
	if !validPlaceholder(placeholder) {
		return Clause{}, ErrInvalidPlaceholder
	}
	id, err := Scope(ctx)
	if err != nil {
		return Clause{}, err
	}
	return Clause{SQL: column + " = " + placeholder, Arg: id}, nil
}

// validColumn reports whether s is an SQL identifier of the form
// part or part.part, where each part is [A-Za-z_][A-Za-z0-9_]*.
func validColumn(s string) bool {
	dots := 0
	atPartStart := true
	for i := range len(s) {
		switch c := s[i]; {
		case c == '.':
			if atPartStart {
				return false
			}
			if dots++; dots > 1 {
				return false
			}
			atPartStart = true
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			atPartStart = false
		case c >= '0' && c <= '9':
			if atPartStart {
				return false
			}
		default:
			return false
		}
	}
	return !atPartStart // rejects "", trailing dot, and empty parts
}

// validPlaceholder reports whether s is "?" or "$n" with n a positive
// decimal without leading zeros.
func validPlaceholder(s string) bool {
	if s == "?" {
		return true
	}
	if len(s) < 2 || s[0] != '$' || s[1] == '0' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
