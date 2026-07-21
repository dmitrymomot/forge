package approval_test

import (
	"testing"

	"github.com/dmitrymomot/forge/ops/approval"
	"github.com/dmitrymomot/forge/ops/approval/approvaltest"
)

func TestMemoryStoreContract(t *testing.T) {
	t.Parallel()
	approvaltest.Run(t, func(t *testing.T) approval.Store {
		return approval.NewMemoryStore()
	})
}
