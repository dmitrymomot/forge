//go:build integration

package mongoqueue_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/queue/brokertest"
	mongoqueue "github.com/dmitrymomot/forge/async/queue/mongo"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/testkit/mongotest"
)

var (
	_ queue.Broker   = (*mongoqueue.Broker)(nil)
	_ queue.TxPusher = (*mongoqueue.Broker)(nil)
)

// runID makes every collection name unique per test process so re-runs
// against a persistent server (FORGE_TEST_MONGO_URI) never collide with prior
// state.
var runID = id.NewULID().String()

// dial connects to the mongo under test: mongotest.URI honors
// FORGE_TEST_MONGO_URI if set, else starts a throwaway single-node
// replica-set container shared across the test process.
func dial(tb testing.TB) *mongodriver.Database {
	tb.Helper()
	client, err := mongodriver.Connect(options.Client().ApplyURI(mongotest.URI(tb)))
	require.NoError(tb, err)
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(ctx)
	})
	return client.Database("forge_queue_test")
}

var collSeq int

// newBroker namespaces each subtest in its own collection; collections leak
// into the ephemeral test server (the mongotest container or a shared real
// mongo), which is acceptable — runID keeps re-runs collision-free.
func newBroker(tb testing.TB) *mongoqueue.Broker {
	tb.Helper()
	collSeq++
	b, err := mongoqueue.New(dial(tb), mongoqueue.WithCollection(fmt.Sprintf("qt_%s_%d", runID, collSeq)))
	require.NoError(tb, err)
	require.NoError(tb, b.EnsureIndexes(context.Background()))
	return b
}

func TestMongoQueue_Conformance(t *testing.T) {
	brokertest.Run(t, func(t *testing.T) queue.Broker { return newBroker(t) })
}

// claimOne polls Claim until the queue yields a job or a deadline passes,
// tolerating slow containers.
func claimOne(t *testing.T, b queue.Broker, q string) queue.ClaimedJob {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := b.Claim(ctx, q, 10, time.Minute)
		require.NoError(t, err)
		if len(got) >= 1 {
			return got[0]
		}
		if time.Now().After(deadline) {
			require.Len(t, got, 1, "expected 1 claimable job within deadline")
			return queue.ClaimedJob{}
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestMongoQueue_PushTx(t *testing.T) {
	db := dial(t)
	collSeq++
	b, err := mongoqueue.New(db, mongoqueue.WithCollection(fmt.Sprintf("qt_%s_tx_%d", runID, collSeq)))
	require.NoError(t, err)
	require.NoError(t, b.EnsureIndexes(context.Background()))
	ctx := context.Background()
	c := queue.NewClient(b)
	kind := queue.NewKind[struct {
		N int `json:"n"`
	}]("tx.kind")

	t.Run("commit makes the job claimable", func(t *testing.T) {
		sess, err := db.Client().StartSession()
		require.NoError(t, err)
		defer sess.EndSession(ctx)
		require.NoError(t, sess.StartTransaction())
		sc := mongodriver.NewSessionContext(ctx, sess)
		require.NoError(t, queue.PushTx(sc, c, sess, kind, struct {
			N int `json:"n"`
		}{N: 1}))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got, "job must be invisible before commit")

		require.NoError(t, sess.CommitTransaction(sc))
		job := claimOne(t, b, "default")
		require.NoError(t, b.Ack(ctx, job.ID, job.Token))
	})

	t.Run("abort discards the job", func(t *testing.T) {
		sess, err := db.Client().StartSession()
		require.NoError(t, err)
		defer sess.EndSession(ctx)
		require.NoError(t, sess.StartTransaction())
		sc := mongodriver.NewSessionContext(ctx, sess)
		require.NoError(t, queue.PushTx(sc, c, sess, kind, struct {
			N int `json:"n"`
		}{N: 2}))
		require.NoError(t, sess.AbortTransaction(sc))

		got, err := b.Claim(ctx, "default", 10, time.Minute)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("wrong tx type errors", func(t *testing.T) {
		err := queue.PushTx(ctx, c, "not a session", kind, struct {
			N int `json:"n"`
		}{N: 3})
		require.Error(t, err)
	})
}

// TestMongoQueue_PushJoinsAmbientSession pins the session-riding idiom the
// package doc promises: a multi-job Push under a caller-owned session-bound
// context must join that transaction instead of starting its own, so an abort
// discards the whole batch.
func TestMongoQueue_PushJoinsAmbientSession(t *testing.T) {
	db := dial(t)
	collSeq++
	// The session below is bound to db's client, so the broker must live on
	// the same client — newBroker would dial a separate one.
	b, err := mongoqueue.New(db, mongoqueue.WithCollection(fmt.Sprintf("qt_%s_ambient_%d", runID, collSeq)))
	require.NoError(t, err)
	require.NoError(t, b.EnsureIndexes(context.Background()))
	ctx := context.Background()

	sess, err := db.Client().StartSession()
	require.NoError(t, err)
	defer sess.EndSession(ctx)
	require.NoError(t, sess.StartTransaction())
	sc := mongodriver.NewSessionContext(ctx, sess)

	jobs := []queue.Job{makeJob("ambient"), makeJob("ambient")}
	require.NoError(t, b.Push(sc, jobs...))
	require.NoError(t, sess.AbortTransaction(sc))

	got, err := b.Claim(ctx, "ambient", 10, time.Minute)
	require.NoError(t, err)
	assert.Empty(t, got, "aborted ambient transaction must discard the batch")
}

func TestMongoQueue_WithCollectionValidation(t *testing.T) {
	db := dial(t)
	_, err := mongoqueue.New(db, mongoqueue.WithCollection("bad$name"))
	require.Error(t, err, "unsafe collection name rejected")
	_, err = mongoqueue.New(db, mongoqueue.WithCollection(""))
	require.Error(t, err, "empty collection name rejected")
}

func TestMongoQueue_PayloadRoundTripsJSON(t *testing.T) {
	b := newBroker(t)
	ctx := context.Background()
	c := queue.NewClient(b)
	require.NoError(t, c.PushRaw(ctx, "raw.kind", json.RawMessage(`{"deep":{"x":[1,2,3]}}`)))
	job := claimOne(t, b, "default")
	assert.JSONEq(t, `{"deep":{"x":[1,2,3]}}`, string(job.Payload))
}

func makeJob(q string) queue.Job {
	return queue.Job{
		ID:          id.NewUUID().String(),
		Queue:       q,
		Type:        "test.kind",
		Payload:     []byte(`{"n":1}`),
		MaxAttempts: 25,
		RunAt:       time.Now().UTC().Add(-2 * time.Second),
		CreatedAt:   time.Now().UTC(),
	}
}

func TestMongoQueue_StatsCapped(t *testing.T) {
	b := newBroker(t)
	ctx := context.Background()

	jobs := make([]queue.Job, 10001)
	for i := range jobs {
		jobs[i] = queue.Job{
			ID: id.NewUUID().String(), Queue: "bulk", Type: "cap.kind",
			Payload: []byte(`{}`), RunAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		}
	}
	require.NoError(t, b.Push(ctx, jobs...))

	st, err := b.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10000, st["bulk"].Pending, "count reports the cap, not the true size")
	assert.True(t, st["bulk"].PendingCapped)
	assert.False(t, st["bulk"].DeadCapped)
}

// TestMongoQueue_PurgeDeadBeforeSpansManyBatches covers the batched drain
// loop that brokertest's PurgeDeadBefore subtest cannot reach: it purges a
// single dead job, so one round always suffices and an off-by-one in the loop
// — a batch silently left behind, or a miscounted total — would go unnoticed.
// 12001 dead documents span two full purgeBatch(5000) rounds plus a short
// one, and the odd document proves the loop's exit does not depend on an
// exact multiple.
//
// The documents are inserted straight into the collection as already-dead
// fixtures rather than pushed, claimed and killed one by one: the sweep reads
// nothing but state and died_at, and 12001 round-trip Kill calls would
// dominate the runtime without testing anything this test is about.
func TestMongoQueue_PurgeDeadBeforeSpansManyBatches(t *testing.T) {
	db := dial(t)
	collSeq++
	collName := fmt.Sprintf("qt_%s_purge_%d", runID, collSeq)
	b, err := mongoqueue.New(db, mongoqueue.WithCollection(collName))
	require.NoError(t, err)
	require.NoError(t, b.EnsureIndexes(context.Background()))
	ctx := context.Background()
	coll := db.Collection(collName)

	const dead = 12001 // > 2x the 5000 batch, deliberately not a multiple of it
	diedAt := time.Now().UTC().Add(-40 * 24 * time.Hour)
	docs := make([]any, dead)
	for i := range docs {
		docs[i] = bson.D{
			{Key: "_id", Value: id.NewUUID().String()},
			{Key: "queue", Value: "purgebatch"},
			{Key: "type", Value: "purge.kind"},
			{Key: "payload", Value: "{}"},
			{Key: "scope", Value: ""},
			{Key: "state", Value: "dead"},
			{Key: "attempt", Value: int32(1)},
			{Key: "max_attempts", Value: int32(5)},
			{Key: "run_at", Value: diedAt},
			{Key: "created_at", Value: diedAt},
			{Key: "died_at", Value: diedAt},
			{Key: "last_error", Value: "retention fixture"},
		}
	}
	_, err = coll.InsertMany(ctx, docs)
	require.NoError(t, err)

	countDead := func() int {
		n, err := coll.CountDocuments(ctx, bson.D{{Key: "state", Value: "dead"}})
		require.NoError(t, err)
		return int(n)
	}
	// Premise guard: without this, an assertion of "everything is gone" could
	// pass on a collection that was never populated.
	require.Equal(t, dead, countDead(), "fixture must fill the dead set")

	// A cutoff before every died_at removes nothing: the loop must exit on
	// the first empty round rather than spin.
	n, err := b.PurgeDeadBefore(ctx, time.Now().Add(-365*24*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n)
	require.Equal(t, dead, countDead(), "a cutoff older than every document must purge nothing")

	n, err = b.PurgeDeadBefore(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, dead, n, "the batched loop must report every purged document exactly once across all rounds")
	assert.Zero(t, countDead(), "no dead document may survive the sweep")

	left, err := b.ListDead(ctx, "purgebatch", 10)
	require.NoError(t, err)
	assert.Empty(t, left)
}
