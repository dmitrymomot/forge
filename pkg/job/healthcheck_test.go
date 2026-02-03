package job

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthcheck_NilManager(t *testing.T) {
	t.Parallel()

	check := Healthcheck(nil)
	err := check(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHealthcheckFailed)
	assert.ErrorIs(t, err, errManagerNil)
}

func TestHealthcheck_NotStarted(t *testing.T) {
	t.Parallel()

	manager := &Manager{
		started:  false,
		registry: newTaskRegistry(),
	}

	check := Healthcheck(manager)
	err := check(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHealthcheckFailed)
	assert.ErrorIs(t, err, errManagerNotStarted)
}
