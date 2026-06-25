package riverdriver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/job"
)

// These tests exercise the pure, unit-testable parts of the River driver that
// do not require a live PostgreSQL connection: the JobInsert -> River
// translation, cron-schedule parsing, and the InsertTx wrong-transaction-type
// guard (which returns before touching the pool). Anything needing a real
// database is covered by integration tests run via `just test-integration`.

func TestTranslateJobInsert(t *testing.T) {
	t.Parallel()

	t.Run("minimal job carries task name and empty opts", func(t *testing.T) {
		t.Parallel()

		args, opts := translateJobInsert(&job.JobInsert{TaskName: "send_email"})

		require.Equal(t, "send_email", args.TaskName)
		require.Equal(t, "forge:task", args.Kind())
		require.Empty(t, args.UniqueKey)
		require.Empty(t, args.Payload)

		// No options set -> zero-value InsertOpts.
		require.Empty(t, opts.Queue)
		require.True(t, opts.ScheduledAt.IsZero())
		require.Zero(t, opts.MaxAttempts)
		require.Zero(t, opts.Priority)
		require.Empty(t, opts.Tags)
		require.Zero(t, opts.UniqueOpts.ByPeriod)
	})

	t.Run("payload and unique key round-trip into args", func(t *testing.T) {
		t.Parallel()

		payload := json.RawMessage(`{"user_id":"u1"}`)
		args, _ := translateJobInsert(&job.JobInsert{
			TaskName:  "task",
			Payload:   payload,
			UniqueKey: "user:u1",
		})

		require.JSONEq(t, `{"user_id":"u1"}`, string(args.Payload))
		require.Equal(t, "user:u1", args.UniqueKey)
	})

	t.Run("all options map onto InsertOpts", func(t *testing.T) {
		t.Parallel()

		scheduledAt := time.Now().Add(time.Hour)
		args, opts := translateJobInsert(&job.JobInsert{
			TaskName:    "task",
			Queue:       "email",
			ScheduledAt: &scheduledAt,
			MaxAttempts: 7,
			Priority:    3,
			Tags:        []string{"a", "b"},
			UniqueFor:   30 * time.Minute,
		})

		require.Equal(t, "task", args.TaskName)
		require.Equal(t, "email", opts.Queue)
		require.WithinDuration(t, scheduledAt, opts.ScheduledAt, time.Second)
		require.Equal(t, 7, opts.MaxAttempts)
		require.Equal(t, 3, opts.Priority)
		require.Equal(t, []string{"a", "b"}, opts.Tags)
		require.Equal(t, 30*time.Minute, opts.UniqueOpts.ByPeriod)
	})

	t.Run("zero-value option fields are not forwarded", func(t *testing.T) {
		t.Parallel()

		// MaxAttempts/Priority of 0 and an empty queue must leave InsertOpts at
		// their zero values so River applies its own defaults.
		_, opts := translateJobInsert(&job.JobInsert{
			TaskName:    "task",
			MaxAttempts: 0,
			Priority:    0,
		})

		require.Empty(t, opts.Queue)
		require.Zero(t, opts.MaxAttempts)
		require.Zero(t, opts.Priority)
		require.Zero(t, opts.UniqueOpts.ByPeriod)
	})
}

func TestParseCronSchedule(t *testing.T) {
	t.Parallel()

	t.Run("valid 5-field expression", func(t *testing.T) {
		t.Parallel()

		schedule, err := parseCronSchedule("0 * * * *")
		require.NoError(t, err)
		require.NotNil(t, schedule)

		// The adapter must compute a sane next time strictly after the input.
		base := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		next := schedule.Next(base)
		require.True(t, next.After(base))
		require.Equal(t, 11, next.Hour())
		require.Equal(t, 0, next.Minute())
	})

	t.Run("descriptor expression", func(t *testing.T) {
		t.Parallel()

		schedule, err := parseCronSchedule("@hourly")
		require.NoError(t, err)
		require.NotNil(t, schedule)

		base := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		next := schedule.Next(base)
		require.True(t, next.After(base))
	})

	t.Run("invalid expression returns error", func(t *testing.T) {
		t.Parallel()

		schedule, err := parseCronSchedule("not a cron expression")
		require.Error(t, err)
		require.Nil(t, schedule)
	})

	t.Run("empty expression returns error", func(t *testing.T) {
		t.Parallel()

		schedule, err := parseCronSchedule("")
		require.Error(t, err)
		require.Nil(t, schedule)
	})
}

func TestInsertTx_InvalidTxType(t *testing.T) {
	t.Parallel()

	// The wrong-transaction-type guard returns before touching the pool, so a
	// nil-pool driver is safe here and no PostgreSQL connection is needed.
	d := New(nil)

	err := d.InsertTx(context.Background(), "not-a-pgx-tx", &job.JobInsert{TaskName: "task"})
	require.ErrorIs(t, err, job.ErrInvalidTx)
	require.ErrorContains(t, err, "expected pgx.Tx")
}

func TestNew_AppliesOptions(t *testing.T) {
	t.Parallel()

	d := New(nil)
	require.NotNil(t, d.logger, "default logger must be set")
	require.False(t, d.started)
}
