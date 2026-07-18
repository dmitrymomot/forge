package mongoqueue

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
)

// state discriminates live jobs (pending or claimed) from dead-lettered ones
// inside the shared collection; partial indexes are filtered on it so each
// side pays only for its own entries.
const (
	stateLive = "live"
	stateDead = "dead"
)

var collectionNameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// statsCap bounds per-queue stats counting: counts are exact up to the cap;
// beyond it QueueStats reports the cap with the Capped flag set. Keeps Stats
// O(cap) via server-side count limits instead of O(collection).
const statsCap = 10000

// purgeBatch bounds one PurgeDeadBefore round (an _id find plus a deleteMany
// over those ids): a huge backlog costs many short commands instead of one
// long deleteMany that a context deadline or socket timeout would interrupt
// with an unknown partial count. 5000 UUID ids is ~200KB of $in filter, far
// inside the 16MB command ceiling.
const purgeBatch = 5000

// jobDoc is the BSON shape of a queue job. claimed_until, claim_token, and
// died_at exist only while relevant (set/unset by the lifecycle updates) and
// are never decoded back into a Job, so they carry no struct fields. Payload
// is stored as a raw JSON string, never re-encoded as BSON, so bytes round-trip
// exactly.
type jobDoc struct {
	RunAt       time.Time `bson:"run_at"`
	CreatedAt   time.Time `bson:"created_at"`
	ID          string    `bson:"_id"`
	Queue       string    `bson:"queue"`
	Type        string    `bson:"type"`
	Scope       string    `bson:"scope"`
	State       string    `bson:"state"`
	Payload     string    `bson:"payload"`
	LastError   string    `bson:"last_error,omitempty"`
	Attempt     int32     `bson:"attempt"`
	MaxAttempts int32     `bson:"max_attempts"`
}

func newDoc(j queue.Job) jobDoc {
	return jobDoc{
		ID:          j.ID,
		Queue:       j.Queue,
		Type:        j.Type,
		Payload:     string(j.Payload),
		Scope:       j.Scope,
		State:       stateLive,
		LastError:   j.LastError,
		Attempt:     int32(j.Attempt),
		MaxAttempts: int32(j.MaxAttempts),
		RunAt:       j.RunAt.UTC(),
		CreatedAt:   j.CreatedAt.UTC(),
	}
}

func (d jobDoc) job() queue.Job {
	return queue.Job{
		ID:          d.ID,
		Queue:       d.Queue,
		Type:        d.Type,
		Payload:     []byte(d.Payload),
		Scope:       d.Scope,
		LastError:   d.LastError,
		Attempt:     int(d.Attempt),
		MaxAttempts: int(d.MaxAttempts),
		RunAt:       d.RunAt,
		CreatedAt:   d.CreatedAt,
	}
}

// Broker is the MongoDB queue.Broker and queue.TxPusher.
type Broker struct {
	coll *mongodriver.Collection
}

// config carries construction-time settings.
type config struct {
	collection string
}

// Option configures New.
type Option func(*config)

// WithCollection overrides the collection name (default "queue_jobs").
func WithCollection(name string) Option {
	return func(c *config) { c.collection = name }
}

// New builds a Broker over db. It performs no I/O; call EnsureIndexes once at
// boot to create the indexes the hot paths depend on.
func New(db *mongodriver.Database, opts ...Option) (*Broker, error) {
	cfg := config{collection: "queue_jobs"}
	for _, opt := range opts {
		opt(&cfg)
	}
	if db == nil {
		return nil, errors.New("mongoqueue: nil database")
	}
	if !collectionNameRe.MatchString(cfg.collection) {
		return nil, fmt.Errorf("mongoqueue: invalid collection name %q", cfg.collection)
	}
	return &Broker{coll: db.Collection(cfg.collection)}, nil
}

// EnsureIndexes idempotently creates the partial indexes the broker relies
// on: the claim scan index over live jobs, the dead-letter listing index, and
// the retention-sweep index. Run once at boot; re-running is a server-side
// no-op.
func (b *Broker) EnsureIndexes(ctx context.Context) error {
	models := []mongodriver.IndexModel{
		{
			Keys: bson.D{{Key: "queue", Value: 1}, {Key: "run_at", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("queue_claim").
				SetPartialFilterExpression(bson.D{{Key: "state", Value: stateLive}}),
		},
		{
			Keys: bson.D{{Key: "queue", Value: 1}, {Key: "died_at", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("queue_dead").
				SetPartialFilterExpression(bson.D{{Key: "state", Value: stateDead}}),
		},
		{
			Keys: bson.D{{Key: "died_at", Value: 1}},
			Options: options.Index().SetName("queue_dead_sweep").
				SetPartialFilterExpression(bson.D{{Key: "state", Value: stateDead}}),
		},
	}
	if _, err := b.coll.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("mongoqueue: ensure indexes: %w", err)
	}
	return nil
}

func docsOf(jobs []queue.Job) []jobDoc {
	docs := make([]jobDoc, len(jobs))
	for i, j := range jobs {
		docs[i] = newDoc(j)
	}
	return docs
}

func (b *Broker) Push(ctx context.Context, jobs ...queue.Job) error {
	switch len(jobs) {
	case 0:
		return nil
	case 1:
		if _, err := b.coll.InsertOne(ctx, newDoc(jobs[0])); err != nil {
			return fmt.Errorf("mongoqueue: push: %w", err)
		}
		return nil
	}
	docs := docsOf(jobs)
	// Inside a caller-owned session (e.g. data/mongo.WithTransaction) the
	// insert joins that transaction; all-or-nothing is the caller's commit.
	if mongodriver.SessionFromContext(ctx) != nil {
		if _, err := b.coll.InsertMany(ctx, docs); err != nil {
			return fmt.Errorf("mongoqueue: push: %w", err)
		}
		return nil
	}
	// InsertMany alone is not all-or-nothing (an ordered insert stops at the
	// first error but keeps what preceded it), so a standalone batch gets its
	// own transaction. Requires a replica set; see the package doc.
	sess, err := b.coll.Database().Client().StartSession()
	if err != nil {
		return fmt.Errorf("mongoqueue: push: %w", err)
	}
	defer sess.EndSession(ctx)
	if _, err := sess.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		return b.coll.InsertMany(ctx, docs)
	}); err != nil {
		return fmt.Errorf("mongoqueue: push: %w", err)
	}
	return nil
}

// PushTx implements queue.TxPusher: the batch insert inside a caller-owned
// *mongo.Session whose transaction the caller has already started (typically
// via Session.WithTransaction), so the jobs commit or abort with the business
// transaction.
func (b *Broker) PushTx(ctx context.Context, tx any, jobs ...queue.Job) error {
	sess, ok := tx.(*mongodriver.Session)
	if !ok || sess == nil {
		return fmt.Errorf("mongoqueue: push tx: expected *mongo.Session, got %T", tx)
	}
	if len(jobs) == 0 {
		return nil
	}
	if _, err := b.coll.InsertMany(mongodriver.NewSessionContext(ctx, sess), docsOf(jobs)); err != nil {
		return fmt.Errorf("mongoqueue: push tx: %w", err)
	}
	return nil
}

// claimable matches live jobs in queueName that are due and not under an
// active lease as of now. claimed_until is unset (never null) on unclaimed
// documents, so $exists:false is the "no lease" arm.
func claimable(queueName string, now time.Time) bson.D {
	return bson.D{
		{Key: "state", Value: stateLive},
		{Key: "queue", Value: queueName},
		{Key: "run_at", Value: bson.D{{Key: "$lte", Value: now}}},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "claimed_until", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "claimed_until", Value: bson.D{{Key: "$lt", Value: now}}}},
		}},
	}
}

var claimSort = bson.D{{Key: "run_at", Value: 1}, {Key: "_id", Value: 1}}

// claimUpdate stamps the lease, the fencing token, and the attempt increment.
func claimUpdate(token string, until time.Time) bson.D {
	return bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "claimed_until", Value: until},
			{Key: "claim_token", Value: token},
		}},
		{Key: "$inc", Value: bson.D{{Key: "attempt", Value: 1}}},
	}
}

func (b *Broker) Claim(ctx context.Context, queueName string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	if n <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	token := id.NewUUID().String()

	// Single-job fast path: findAndModify claims, stamps, and returns the
	// post-update document atomically in one round trip instead of the
	// three-step batch below. Measured ~19% faster on the push/claim/ack
	// cycle (1.61ms -> 1.31ms per op) with 41% fewer allocations; see
	// bench_test.go.
	if n == 1 {
		var d jobDoc
		err := b.coll.FindOneAndUpdate(ctx, claimable(queueName, now), claimUpdate(token, now.Add(lease)),
			options.FindOneAndUpdate().SetSort(claimSort).SetReturnDocument(options.After)).Decode(&d)
		switch {
		case errors.Is(err, mongodriver.ErrNoDocuments):
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("mongoqueue: claim: %w", err)
		}
		return []queue.ClaimedJob{{Job: d.job(), Token: token}}, nil
	}

	// Round trip 1: candidate ids in (run_at, _id) order off the partial
	// claim index. Not yet a claim — just the shortlist.
	cur, err := b.coll.Find(ctx, claimable(queueName, now), options.Find().
		SetSort(claimSort).
		SetLimit(int64(n)).
		SetProjection(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("mongoqueue: claim find: %w", err)
	}
	var candidates []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &candidates); err != nil {
		return nil, fmt.Errorf("mongoqueue: claim decode: %w", err)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}

	// Round trip 2: claim atomically per document. The guard repeats the
	// claimable predicate so a candidate won by a concurrent claimer since
	// round trip 1 is skipped, not stolen; each matched document gets the
	// lease, the batch token, and its attempt increment in one atomic update.
	guard := append(bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}}, claimable(queueName, now)...)
	res, err := b.coll.UpdateMany(ctx, guard, claimUpdate(token, now.Add(lease)))
	if err != nil {
		return nil, fmt.Errorf("mongoqueue: claim update: %w", err)
	}
	if res.ModifiedCount == 0 {
		return nil, nil
	}

	// Round trip 3: fetch the documents this token actually won, post-update
	// (attempt already incremented), via the _id index.
	cur, err = b.coll.Find(ctx, bson.D{
		{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}},
		{Key: "claim_token", Value: token},
	}, options.Find().SetSort(claimSort))
	if err != nil {
		return nil, fmt.Errorf("mongoqueue: claim fetch: %w", err)
	}
	var docs []jobDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("mongoqueue: claim fetch decode: %w", err)
	}
	claimed := make([]queue.ClaimedJob, len(docs))
	for i, d := range docs {
		claimed[i] = queue.ClaimedJob{Job: d.job(), Token: token}
	}
	return claimed, nil
}

// fenced is the ownership filter for Extend/Ack/Nack/Kill: the update or
// delete applies only while token still holds the claim.
func fenced(jobID, token string) bson.D {
	return bson.D{
		{Key: "_id", Value: jobID},
		{Key: "state", Value: stateLive},
		{Key: "claim_token", Value: token},
	}
}

func (b *Broker) Extend(ctx context.Context, jobID, token string, lease time.Duration) error {
	res, err := b.coll.UpdateOne(ctx, fenced(jobID, token), bson.D{
		{Key: "$set", Value: bson.D{{Key: "claimed_until", Value: time.Now().UTC().Add(lease)}}},
	})
	if err != nil {
		return fmt.Errorf("mongoqueue: extend: %w", err)
	}
	if res.MatchedCount == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, jobID, token string) error {
	res, err := b.coll.DeleteOne(ctx, fenced(jobID, token))
	if err != nil {
		return fmt.Errorf("mongoqueue: ack: %w", err)
	}
	if res.DeletedCount == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, jobID, token string, retryAt time.Time, reason string) error {
	res, err := b.coll.UpdateOne(ctx, fenced(jobID, token), bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "run_at", Value: retryAt.UTC()},
			{Key: "last_error", Value: reason},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "claimed_until", Value: ""},
			{Key: "claim_token", Value: ""},
		}},
	})
	if err != nil {
		return fmt.Errorf("mongoqueue: nack: %w", err)
	}
	if res.MatchedCount == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, jobID, token string, reason string) error {
	res, err := b.coll.UpdateOne(ctx, fenced(jobID, token), bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "state", Value: stateDead},
			{Key: "died_at", Value: time.Now().UTC()},
			{Key: "last_error", Value: reason},
		}},
		{Key: "$unset", Value: bson.D{
			{Key: "claimed_until", Value: ""},
			{Key: "claim_token", Value: ""},
		}},
	})
	if err != nil {
		return fmt.Errorf("mongoqueue: kill: %w", err)
	}
	if res.MatchedCount == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) ListDead(ctx context.Context, queueName string, limit int) ([]queue.Job, error) {
	if limit <= 0 {
		return nil, nil
	}
	cur, err := b.coll.Find(ctx, bson.D{
		{Key: "state", Value: stateDead},
		{Key: "queue", Value: queueName},
	}, options.Find().
		SetSort(bson.D{{Key: "died_at", Value: 1}, {Key: "_id", Value: 1}}).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("mongoqueue: list dead: %w", err)
	}
	var docs []jobDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("mongoqueue: list dead decode: %w", err)
	}
	jobs := make([]queue.Job, len(docs))
	for i, d := range docs {
		jobs[i] = d.job()
	}
	return jobs, nil
}

func (b *Broker) Requeue(ctx context.Context, jobID string) error {
	res, err := b.coll.UpdateOne(ctx, bson.D{
		{Key: "_id", Value: jobID},
		{Key: "state", Value: stateDead},
	}, bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "state", Value: stateLive},
			{Key: "attempt", Value: int32(0)},
			{Key: "run_at", Value: time.Now().UTC()},
		}},
		{Key: "$unset", Value: bson.D{{Key: "died_at", Value: ""}}},
	})
	if err != nil {
		return fmt.Errorf("mongoqueue: requeue: %w", err)
	}
	if res.MatchedCount == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, jobID string) error {
	res, err := b.coll.DeleteOne(ctx, bson.D{
		{Key: "_id", Value: jobID},
		{Key: "state", Value: stateDead},
	})
	if err != nil {
		return fmt.Errorf("mongoqueue: purge: %w", err)
	}
	if res.DeletedCount == 0 {
		return b.notDeadOrMissing(ctx, jobID)
	}
	return nil
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	total := 0
	// Drain in bounded rounds so a large backlog costs many short commands
	// instead of one long delete; a backlog too large for one tick simply
	// continues on the next.
	for {
		cur, err := b.coll.Find(ctx, bson.D{
			{Key: "state", Value: stateDead},
			{Key: "died_at", Value: bson.D{{Key: "$lt", Value: cutoff.UTC()}}},
		}, options.Find().
			SetLimit(purgeBatch).
			SetProjection(bson.D{{Key: "_id", Value: 1}}))
		if err != nil {
			return total, fmt.Errorf("mongoqueue: purge dead before: %w", err)
		}
		var docs []struct {
			ID string `bson:"_id"`
		}
		if err := cur.All(ctx, &docs); err != nil {
			return total, fmt.Errorf("mongoqueue: purge dead before decode: %w", err)
		}
		if len(docs) == 0 {
			return total, nil
		}
		ids := make([]string, len(docs))
		for i, d := range docs {
			ids[i] = d.ID
		}
		// state repeated in the delete filter: a concurrent Requeue between
		// the find and the delete makes the job live again, and a live job
		// must never be swept.
		res, err := b.coll.DeleteMany(ctx, bson.D{
			{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}},
			{Key: "state", Value: stateDead},
		})
		if err != nil {
			return total, fmt.Errorf("mongoqueue: purge dead before: %w", err)
		}
		total += int(res.DeletedCount)
		if len(docs) < purgeBatch {
			// Short round: nothing left under the cutoff beyond this batch.
			// A concurrent sweeper may have won some of these ids; total stays
			// honest about what we removed, and whoever won keeps draining.
			return total, nil
		}
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
	}
}

func (b *Broker) notDeadOrMissing(ctx context.Context, jobID string) error {
	n, err := b.coll.CountDocuments(ctx, bson.D{{Key: "_id", Value: jobID}}, options.Count().SetLimit(1))
	if err != nil {
		return fmt.Errorf("mongoqueue: exists: %w", err)
	}
	if n > 0 {
		return queue.ErrNotDead
	}
	return queue.ErrJobNotFound
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	st := make(queue.Stats)
	if err := b.statsInto(ctx, st, stateLive); err != nil {
		return nil, err
	}
	if err := b.statsInto(ctx, st, stateDead); err != nil {
		return nil, err
	}
	return st, nil
}

// statsInto merges one state's per-queue counts into st. Counts run with a
// server-side limit of statsCap+1: a full statsCap+1 result means "more than
// the cap", reported as the cap with the Capped flag set.
func (b *Broker) statsInto(ctx context.Context, st queue.Stats, state string) error {
	var queues []string
	if err := b.coll.Distinct(ctx, "queue", bson.D{{Key: "state", Value: state}}).Decode(&queues); err != nil {
		return fmt.Errorf("mongoqueue: stats distinct: %w", err)
	}
	for _, q := range queues {
		n, err := b.coll.CountDocuments(ctx, bson.D{
			{Key: "state", Value: state},
			{Key: "queue", Value: q},
		}, options.Count().SetLimit(statsCap+1))
		if err != nil {
			return fmt.Errorf("mongoqueue: stats count: %w", err)
		}
		capped := n > statsCap
		if capped {
			n = statsCap
		}
		qs := st[q]
		if state == stateDead {
			qs.Dead, qs.DeadCapped = int(n), capped
		} else {
			qs.Pending, qs.PendingCapped = int(n), capped
		}
		st[q] = qs
	}
	return nil
}
