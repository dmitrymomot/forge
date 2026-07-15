package queue_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/async/queue"
)

func sampleJob() queue.Job {
	return queue.Job{
		ID:          "01J0000000000000000000TEST",
		Queue:       "critical",
		Type:        "email.send_welcome",
		Payload:     []byte(`{"user_id":"u1"}`),
		Scope:       "tenant-a",
		Attempt:     3,
		MaxAttempts: 25,
		RunAt:       time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		CreatedAt:   time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC),
		LastError:   "boom",
	}
}

func TestCodec_RoundTrip(t *testing.T) {
	t.Parallel()
	in := sampleJob()
	b, err := queue.EncodeJob(in)
	require.NoError(t, err)
	out, err := queue.DecodeJob(b)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestCodec_RoundTripZeroTimes(t *testing.T) {
	t.Parallel()
	in := queue.Job{ID: "x", Queue: "default", Type: "t", Payload: []byte(`{}`)}
	b, err := queue.EncodeJob(in)
	require.NoError(t, err)
	out, err := queue.DecodeJob(b)
	require.NoError(t, err)
	assert.True(t, out.RunAt.IsZero())
	assert.True(t, out.CreatedAt.IsZero())
	assert.Equal(t, in, out)
}

func TestCodec_RejectsUnknownVersion(t *testing.T) {
	t.Parallel()
	_, err := queue.DecodeJob([]byte(`{"v":99,"id":"x"}`))
	require.Error(t, err)
}

func TestCodec_RejectsGarbage(t *testing.T) {
	t.Parallel()
	_, err := queue.DecodeJob([]byte(`not json`))
	require.Error(t, err)
}
