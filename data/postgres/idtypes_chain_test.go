package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recordingHook(log *[]string, name string, err error) afterConnectHook {
	return func(context.Context, *pgx.Conn) error {
		*log = append(*log, name)
		return err
	}
}

// TestChainAfterConnect_KeepsTheConsumerHook is the regression: WithPoolConfig's
// documented purpose includes setting AfterConnect, and assigning that field must not
// drop the id-type registration.
func TestChainAfterConnect_KeepsTheConsumerHook(t *testing.T) {
	var log []string
	chained := chainAfterConnect(
		recordingHook(&log, "register", nil),
		recordingHook(&log, "consumer", nil),
	)

	require.NoError(t, chained(t.Context(), nil))
	assert.Equal(t, []string{"register", "consumer"}, log)
}

func TestChainAfterConnect_NilPrevReturnsFirstAlone(t *testing.T) {
	var log []string
	first := recordingHook(&log, "register", nil)

	require.NoError(t, chainAfterConnect(first, nil)(t.Context(), nil))
	assert.Equal(t, []string{"register"}, log)
}

func TestChainAfterConnect_FailingFirstSkipsPrev(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	chained := chainAfterConnect(
		recordingHook(&log, "register", boom),
		recordingHook(&log, "consumer", nil),
	)

	assert.ErrorIs(t, chained(t.Context(), nil), boom)
	assert.Equal(t, []string{"register"}, log)
}

func TestChainIDTypeRegistration_AlwaysSetsAHook(t *testing.T) {
	poolCfg, err := pgxpool.ParseConfig("postgres://u:p@localhost:5432/db")
	require.NoError(t, err)
	require.Nil(t, poolCfg.AfterConnect)

	chainIDTypeRegistration(poolCfg)
	assert.NotNil(t, poolCfg.AfterConnect)
}

// TestBuildPoolConfig_RegistrationSurvivesTheEscapeHatch is the regression itself:
// WithPoolConfig's documented purpose includes assigning AfterConnect, so the
// registration has to be chained on after that hook runs, not before it.
func TestBuildPoolConfig_RegistrationSurvivesTheEscapeHatch(t *testing.T) {
	var log []string
	cfg := config{Config: Config{URL: "postgres://u:p@localhost:5432/db"}}
	cfg.poolConfig = func(pc *pgxpool.Config) {
		pc.AfterConnect = recordingHook(&log, "consumer", nil)
	}

	poolCfg, err := buildPoolConfig(cfg)
	require.NoError(t, err)
	require.NotNil(t, poolCfg.AfterConnect)

	require.NoError(t, poolCfg.AfterConnect(t.Context(), nil))
	assert.Equal(t, []string{"consumer"}, log, "the consumer hook must still run")
}
