package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/ops/logger"
)

const group = "workers"

var promoteScript = redis.NewScript(`
local due = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, tonumber(ARGV[2]))
for i, id in ipairs(due) do
  local data = redis.call('HGET', KEYS[2], id)
  if data then
    redis.call('XADD', KEYS[3], '*', 'j', data)
  end
  redis.call('ZREM', KEYS[1], id)
  redis.call('HDEL', KEYS[2], id)
end
return #due
`)

// Finalize scripts verify PEL ownership atomically before mutating: XPENDING
// for the exact message must name this consumer, otherwise the message was
// autoclaimed by another worker (or already finalized) and the op returns 0 →
// ErrLeaseLost. Plain XACK would succeed regardless of owner.
var ackScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
redis.call('HDEL', KEYS[2], ARGV[4])
return 1
`)

var nackScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('ZADD', KEYS[2], ARGV[5], ARGV[4])
redis.call('HSET', KEYS[3], ARGV[4], ARGV[6])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
return 1
`)

var killScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('HSET', KEYS[2], ARGV[4], ARGV[5])
redis.call('ZADD', KEYS[3], ARGV[6], ARGV[4])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[3])
redis.call('XDEL', KEYS[1], ARGV[3])
return 1
`)

var extendScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[3], ARGV[3], 1)
if #p == 0 or p[1][2] ~= ARGV[2] then return 0 end
redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, ARGV[3], 'JUSTID')
return 1
`)

// requeueScript atomically moves a dead job back to the stream. HDEL-as-test:
// only the caller that actually removes the dead entry re-adds the job, so a
// concurrent double-requeue cannot duplicate it.
var requeueScript = redis.NewScript(`
if redis.call('HDEL', KEYS[1], ARGV[1]) == 0 then return 0 end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('XADD', KEYS[3], '*', 'j', ARGV[2])
return 1
`)

var purgeScript = redis.NewScript(`
if redis.call('HDEL', KEYS[1], ARGV[1]) == 0 then return 0 end
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('HDEL', KEYS[3], ARGV[1])
return 1
`)

// purgeDeadBeforeScript removes ONE bounded slice of expired dead jobs; the
// caller loops. The LIMIT is load-bearing, not tidiness: redis is
// single-threaded and a script is one atomic unit, so an unbounded
// ZRANGEBYSCORE would materialize the whole expired set into a Lua table and
// then run three commands per id in a single exec. DeadRetention is new, so
// the first sweep after an upgrade can face an entire never-purged DLQ —
// millions of ids, millions of commands, one exec. Past
// busy-reply-threshold redis answers -BUSY to every other client, and a
// script that has already written refuses SCRIPT KILL: the only way out is
// SHUTDOWN NOSAVE, which discards the stream. Mirrors promoteScript's shape.
var purgeDeadBeforeScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, tonumber(ARGV[2]))
for i = 1, #ids do
  redis.call('ZREM', KEYS[1], ids[i])
  redis.call('HDEL', KEYS[2], ids[i])
  redis.call('HDEL', KEYS[3], ids[i])
end
return #ids
`)

// purgeBatch bounds one purgeDeadBeforeScript exec: 500 ids ≈ 1500 commands,
// comfortably sub-millisecond, four orders of magnitude under the default 5s
// busy-reply-threshold. Larger than promoteScript's 128 because retention is a
// 5-minute background sweep that wants throughput on a backlog, not the
// per-Claim latency promote is paying for.
const purgeBatch = 500

// deliveryCountsScript reads the exact PEL delivery counter for each claimed
// message, in one round trip. A single ranged XPENDING cannot do this job:
// XAUTOCLAIM skips entries below MinIdle, so the redelivered set is generally
// NON-CONTIGUOUS in the PEL, while XPENDING walks every consumer's entries in
// ID order and stops at COUNT. Any interleaved entry — another worker's live
// job, or this instance's own in-flight one — eats a slot and truncates the
// tail, which then reads as "no counter" and undercounts the attempt. Asking
// per message is exact for both cases; the loop is bounded by the claim batch,
// unlike a whole-PEL scan. KEYS[1]=stream, ARGV[1]=group, ARGV[2..]=msg IDs.
var deliveryCountsScript = redis.NewScript(`
local out = {}
for i = 2, #ARGV do
  local p = redis.call('XPENDING', KEYS[1], ARGV[1], ARGV[i], ARGV[i], 1)
  out[#out+1] = (#p > 0) and p[1][4] or 0
end
return out
`)

// poisonScript parks a raw undecodable entry and removes it from the stream so
// one bad entry (foreign XADD, future wire version) cannot wedge Claim forever.
var poisonScript = redis.NewScript(`
redis.call('RPUSH', KEYS[2], ARGV[3])
redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
redis.call('XDEL', KEYS[1], ARGV[2])
return 1
`)

// delConsumerScript checks-then-deletes a consumer atomically: XGROUP
// DELCONSUMER discards the consumer's PEL entries outright rather than
// reassigning them, so a plain read-then-delete could wipe out an entry
// delivered in the gap between the pending snapshot and the delete — gone
// from the group forever, never redelivered, never claimable, never
// dead-lettered. Folding the pending check into the delete closes that
// window, matching every other finalize op in this file.
var delConsumerScript = redis.NewScript(`
local p = redis.call('XPENDING', KEYS[1], ARGV[1], '-', '+', 1, ARGV[2])
if #p > 0 then return 0 end
redis.call('XGROUP', 'DELCONSUMER', KEYS[1], ARGV[1], ARGV[2])
return 1
`)

// Broker is the Redis queue.Broker.
type Broker struct {
	client     redis.UniversalClient
	claimed    map[string]claimedRef // job id → ref; only the claiming instance finalizes
	groups     map[string]bool       // queues with the consumer group ensured
	log        *slog.Logger
	prefix     string
	consumer   string
	idleCutoff time.Duration

	mu sync.Mutex
}

type claimedRef struct {
	msgID string
	queue string
	token string
	job   queue.Job // post-claim envelope: Attempt is what the engine saw this round (incl. crash redeliveries)
}

// Option configures New.
type Option func(*Broker)

// WithPrefix overrides the key prefix (default "queue:").
func WithPrefix(p string) Option {
	return func(b *Broker) { b.prefix = p }
}

// WithLogger sets the logger (default logger.NewNope()); used for poison
// parking and maintenance reporting.
func WithLogger(l *slog.Logger) Option {
	return func(b *Broker) { b.log = l }
}

// WithConsumerIdleCutoff overrides how long a consumer with no pending
// entries must be idle before Maintain deletes it (default 1h).
func WithConsumerIdleCutoff(d time.Duration) Option {
	return func(b *Broker) { b.idleCutoff = d }
}

// New builds a Broker over client.
func New(client redis.UniversalClient, opts ...Option) (*Broker, error) {
	host, _ := os.Hostname()
	b := &Broker{
		client:     client,
		prefix:     "queue:",
		consumer:   fmt.Sprintf("%s-%d-%s", host, os.Getpid(), id.NewULID().String()),
		claimed:    make(map[string]claimedRef),
		groups:     make(map[string]bool),
		log:        logger.NewNope(),
		idleCutoff: time.Hour,
	}
	for _, opt := range opts {
		opt(b)
	}
	if client == nil {
		return nil, errors.New("redisqueue: nil client")
	}
	return b, nil
}

func (b *Broker) streamKey(q string) string  { return b.prefix + q }
func (b *Broker) delayedKey(q string) string { return b.prefix + q + ":delayed" }
func (b *Broker) dataKey(q string) string    { return b.prefix + q + ":data" }
func (b *Broker) deadKey(q string) string    { return b.prefix + q + ":dead" }
func (b *Broker) deadIdxKey(q string) string { return b.prefix + q + ":dead:idx" }
func (b *Broker) queuesKey() string          { return b.prefix + "queues" }
func (b *Broker) indexKey() string           { return b.prefix + "index" }
func (b *Broker) poisonKey(q string) string  { return b.prefix + q + ":poison" }

func (b *Broker) ensureGroup(ctx context.Context, q string) error {
	b.mu.Lock()
	done := b.groups[q]
	b.mu.Unlock()
	if done {
		return nil
	}
	err := b.client.XGroupCreateMkStream(ctx, b.streamKey(q), group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("redisqueue: create group: %w", err)
	}
	b.mu.Lock()
	b.groups[q] = true
	b.mu.Unlock()
	return nil
}

func (b *Broker) Push(ctx context.Context, jobs ...queue.Job) error {
	if len(jobs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(jobs))
	for _, j := range jobs {
		if seen[j.Queue] {
			continue
		}
		seen[j.Queue] = true
		if err := b.ensureGroup(ctx, j.Queue); err != nil {
			return err
		}
	}
	pipe := b.client.TxPipeline()
	for _, j := range jobs {
		enc, err := queue.EncodeJob(j)
		if err != nil {
			return err
		}
		pipe.SAdd(ctx, b.queuesKey(), j.Queue)
		pipe.HSet(ctx, b.indexKey(), j.ID, j.Queue)
		if j.RunAt.After(time.Now()) {
			pipe.ZAdd(ctx, b.delayedKey(j.Queue), redis.Z{Score: float64(j.RunAt.UnixMilli()), Member: j.ID})
			pipe.HSet(ctx, b.dataKey(j.Queue), j.ID, enc)
		} else {
			pipe.XAdd(ctx, &redis.XAddArgs{Stream: b.streamKey(j.Queue), Values: map[string]any{"j": enc}})
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: push: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, q string, n int, lease time.Duration) ([]queue.ClaimedJob, error) {
	if err := b.ensureGroup(ctx, q); err != nil {
		return nil, err
	}
	// Promote due delayed/retried jobs into the stream.
	nowMS := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := promoteScript.Run(ctx, b.client, []string{b.delayedKey(q), b.dataKey(q), b.streamKey(q)}, nowMS, "128").Err(); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: promote delayed: %w", err)
	}

	// One fencing token per Claim call, shared by every job in this batch.
	token := id.NewUUID().String()

	remaining := n
	var out []queue.ClaimedJob

	// Lease-expired redeliveries first.
	msgs, _, err := b.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: b.streamKey(q), Group: group, Consumer: b.consumer,
		MinIdle: lease, Start: "0-0", Count: int64(remaining),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: autoclaim: %w", err)
	}
	if len(msgs) > 0 {
		args := make([]any, 0, len(msgs)+1)
		args = append(args, group)
		for _, m := range msgs {
			args = append(args, m.ID)
		}
		counts, err := deliveryCountsScript.Run(ctx, b.client, []string{b.streamKey(q)}, args...).Int64Slice()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: pending: %w", err)
		}
		if len(counts) != len(msgs) {
			return nil, fmt.Errorf("redisqueue: pending: got %d delivery counters for %d autoclaimed messages", len(counts), len(msgs))
		}
		for i, m := range msgs {
			j, err := b.decodeMsg(m)
			if err != nil {
				b.park(ctx, q, m, err)
				continue
			}
			// A zero counter means the entry is no longer in the PEL despite
			// the XAUTOCLAIM above: someone finalized it in the gap. Dispatch
			// would duplicate work whose finalize is already doomed to
			// ErrLeaseLost, so skip it — the entry is not ours to run. No job
			// is lost: an entry that IS still ours redelivers once its lease
			// lapses again. Never fabricate a count here; that is precisely
			// what used to make a missing counter indistinguishable from a
			// first delivery.
			if counts[i] == 0 {
				b.log.WarnContext(ctx, "redisqueue: autoclaimed entry vanished from the pending list, skipping",
					slog.String("queue", q), slog.String("msg_id", m.ID), slog.String("job_id", j.ID))
				continue
			}
			claimedJob := j
			claimedJob.Attempt = j.Attempt + int(counts[i])
			b.remember(claimedJob.ID, claimedRef{job: claimedJob, msgID: m.ID, queue: q, token: token})
			out = append(out, queue.ClaimedJob{Job: claimedJob, Token: token})
		}
		// Decrement by jobs actually claimed, not len(msgs): parked
		// (undecodable) and vanished (zero-counter) entries above hit
		// continue without reaching the append, so they consumed a message
		// but never became a job. Charging them against remaining would
		// under-fill this batch below when the stream had more room to give.
		remaining -= len(out)
	}

	// Fresh deliveries.
	if remaining > 0 {
		streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: group, Consumer: b.consumer,
			Streams: []string{b.streamKey(q), ">"}, Count: int64(remaining), Block: -1,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: readgroup: %w", err)
		}
		for _, st := range streams {
			for _, m := range st.Messages {
				j, err := b.decodeMsg(m)
				if err != nil {
					b.park(ctx, q, m, err)
					continue
				}
				claimedJob := j
				claimedJob.Attempt = j.Attempt + 1
				b.remember(claimedJob.ID, claimedRef{job: claimedJob, msgID: m.ID, queue: q, token: token})
				out = append(out, queue.ClaimedJob{Job: claimedJob, Token: token})
			}
		}
	}
	return out, nil
}

func (b *Broker) decodeMsg(m redis.XMessage) (queue.Job, error) {
	raw, ok := m.Values["j"].(string)
	if !ok {
		return queue.Job{}, fmt.Errorf("redisqueue: stream entry %s has no payload field", m.ID)
	}
	j, err := queue.DecodeJob([]byte(raw))
	if err != nil {
		return queue.Job{}, err
	}
	return j, nil
}

// park moves an undecodable stream entry to the queue's poison list. The
// entry is already in this consumer's PEL (just read or autoclaimed), so the
// ack inside the script is ours to perform.
func (b *Broker) park(ctx context.Context, q string, m redis.XMessage, decErr error) {
	raw, _ := m.Values["j"].(string)
	if raw == "" {
		raw = fmt.Sprintf("unparseable stream entry %s: %v", m.ID, m.Values)
	}
	if err := poisonScript.Run(ctx, b.client, []string{b.streamKey(q), b.poisonKey(q)}, group, m.ID, raw).Err(); err != nil {
		b.log.ErrorContext(ctx, "redisqueue: poison park failed", slog.String("queue", q), slog.String("msg_id", m.ID), slog.Any("error", err))
		return
	}
	b.log.ErrorContext(ctx, "redisqueue: undecodable entry parked to poison list", slog.String("queue", q), slog.String("msg_id", m.ID), slog.Any("error", decErr))
}

func (b *Broker) remember(id string, ref claimedRef) {
	b.mu.Lock()
	b.claimed[id] = ref
	b.mu.Unlock()
}

// take removes and returns the claimed ref for id IF token owns it. A
// mismatched token means the ref belongs to a newer claim on this instance —
// left untouched, the caller gets ErrLeaseLost.
func (b *Broker) take(id, token string) (claimedRef, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ref, ok := b.claimed[id]
	if !ok || ref.token != token {
		return claimedRef{}, false
	}
	delete(b.claimed, id)
	return ref, true
}

func (b *Broker) Extend(ctx context.Context, jobID, token string, _ time.Duration) error {
	b.mu.Lock()
	ref, ok := b.claimed[jobID]
	b.mu.Unlock()
	if !ok || ref.token != token {
		return queue.ErrLeaseLost
	}
	// XCLAIM JUSTID (to ourselves, inside the ownership check) resets the idle
	// clock without bumping the delivery counter. Lease expiry is idle-based
	// here: the next Claim's MinIdle is the lease.
	n, err := extendScript.Run(ctx, b.client, []string{b.streamKey(ref.queue)}, group, b.consumer, ref.msgID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: extend: %w", err)
	}
	if n == 0 {
		b.mu.Lock()
		delete(b.claimed, jobID) // stale ref: message moved to another consumer
		b.mu.Unlock()
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, jobID, token string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	n, err := ackScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.indexKey()},
		group, b.consumer, ref.msgID, jobID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: ack: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, jobID, token string, retryAt time.Time, reason string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	j := ref.job
	// ref.job.Attempt already reflects the attempt the engine just consumed
	// (including crash redeliveries via XAUTOCLAIM), so persist it as-is.
	j.LastError = reason
	j.RunAt = retryAt.UTC()
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	n, err := nackScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.delayedKey(ref.queue), b.dataKey(ref.queue)},
		group, b.consumer, ref.msgID, jobID, retryAt.UnixMilli(), enc).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: nack: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, jobID, token string, reason string) error {
	ref, ok := b.take(jobID, token)
	if !ok {
		return queue.ErrLeaseLost
	}
	j := ref.job // Attempt already reflects the consumed attempt (incl. crash redeliveries)
	j.LastError = reason
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	n, err := killScript.Run(ctx, b.client,
		[]string{b.streamKey(ref.queue), b.deadKey(ref.queue), b.deadIdxKey(ref.queue)},
		group, b.consumer, ref.msgID, jobID, enc, time.Now().UnixMilli()).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: kill: %w", err)
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

func (b *Broker) ListDead(ctx context.Context, q string, limit int) ([]queue.Job, error) {
	if limit <= 0 {
		return nil, nil
	}
	// The idx ZSET is scored by kill-time ms with lexicographic id tiebreak,
	// so a range read IS the ListDead order; O(limit), never O(DLQ).
	ids, err := b.client.ZRange(ctx, b.deadIdxKey(q), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead range: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	encs, err := b.client.HMGet(ctx, b.deadKey(q), ids...).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead fetch: %w", err)
	}
	jobs := make([]queue.Job, 0, len(encs))
	for _, enc := range encs {
		s, ok := enc.(string)
		if !ok {
			continue // purged between ZRANGE and HMGET
		}
		j, err := queue.DecodeJob([]byte(s))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (b *Broker) Requeue(ctx context.Context, jobID string) error {
	q, err := b.client.HGet(ctx, b.indexKey(), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("redisqueue: requeue index: %w", err)
	}
	enc, err := b.client.HGet(ctx, b.deadKey(q), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrNotDead
	}
	if err != nil {
		return fmt.Errorf("redisqueue: requeue dead: %w", err)
	}
	j, err := queue.DecodeJob([]byte(enc))
	if err != nil {
		return err
	}
	j.Attempt = 0
	j.RunAt = time.Now().UTC()
	fresh, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	n, err := requeueScript.Run(ctx, b.client,
		[]string{b.deadKey(q), b.deadIdxKey(q), b.streamKey(q)}, jobID, fresh).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: requeue: %w", err)
	}
	if n == 0 {
		return queue.ErrNotDead // lost a concurrent requeue/purge race
	}
	return nil
}

func (b *Broker) Purge(ctx context.Context, jobID string) error {
	q, err := b.client.HGet(ctx, b.indexKey(), jobID).Result()
	if errors.Is(err, redis.Nil) {
		return queue.ErrJobNotFound
	}
	if err != nil {
		return fmt.Errorf("redisqueue: purge index: %w", err)
	}
	n, err := purgeScript.Run(ctx, b.client,
		[]string{b.deadKey(q), b.deadIdxKey(q), b.indexKey()}, jobID).Int()
	if err != nil {
		return fmt.Errorf("redisqueue: purge: %w", err)
	}
	if n == 0 {
		return queue.ErrNotDead
	}
	return nil
}

func (b *Broker) PurgeDeadBefore(ctx context.Context, cutoff time.Time) (int, error) {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return 0, fmt.Errorf("redisqueue: purge dead before: %w", err)
	}
	total := 0
	// "(" makes the score bound exclusive: died_at < cutoff, not <=.
	cutoffArg := fmt.Sprintf("(%d", cutoff.UnixMilli())
	for _, q := range queues {
		keys := []string{b.deadIdxKey(q), b.deadKey(q), b.indexKey()}
		// Drain in bounded batches: each exec is short, the sweep stays
		// interruptible, and a backlog too large for one tick simply
		// continues on the next.
		for {
			n, err := purgeDeadBeforeScript.Run(ctx, b.client, keys, cutoffArg, purgeBatch).Int()
			if err != nil {
				return total, fmt.Errorf("redisqueue: purge dead before %q: %w", q, err)
			}
			total += n
			if n < purgeBatch {
				break // short batch: nothing left under the cutoff
			}
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
		}
	}
	return total, nil
}

func (b *Broker) Stats(ctx context.Context) (queue.Stats, error) {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: stats queues: %w", err)
	}
	st := make(queue.Stats, len(queues))
	for _, q := range queues {
		streamLen, err := b.client.XLen(ctx, b.streamKey(q)).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: stats xlen: %w", err)
		}
		delayed, err := b.client.ZCard(ctx, b.delayedKey(q)).Result()
		if err != nil {
			return nil, fmt.Errorf("redisqueue: stats zcard: %w", err)
		}
		dead, err := b.client.HLen(ctx, b.deadKey(q)).Result()
		if err != nil {
			return nil, fmt.Errorf("redisqueue: stats hlen: %w", err)
		}
		st[q] = queue.QueueStats{Pending: int(streamLen + delayed), Dead: int(dead)}
	}
	return st, nil
}

// Maintain implements queue.Maintainer: deletes consumers that have no
// pending entries and have been idle past the cutoff (each process registers
// a unique consumer name, so restarts accumulate them forever otherwise), and
// prunes queues whose stream, delayed set, dead store, and poison list are all
// empty from the queues registry so Stats stops probing them. Safe to run
// concurrently from every worker instance.
func (b *Broker) Maintain(ctx context.Context) error {
	queues, err := b.client.SMembers(ctx, b.queuesKey()).Result()
	if err != nil {
		return fmt.Errorf("redisqueue: maintain queues: %w", err)
	}
	for _, q := range queues {
		consumers, err := b.client.XInfoConsumers(ctx, b.streamKey(q), group).Result()
		if err != nil && !strings.Contains(err.Error(), "NOGROUP") && !strings.Contains(err.Error(), "no such key") {
			return fmt.Errorf("redisqueue: maintain consumers %q: %w", q, err)
		}
		for _, c := range consumers {
			// c.Pending is a snapshot, not checked here: a stale zero would
			// race the delete against a fresh delivery (see delConsumerScript).
			// A stale c.Idle is harmless either way — a zero-pending consumer
			// that loses its registration simply re-registers on its next read.
			if c.Name == b.consumer || c.Idle < b.idleCutoff {
				continue
			}
			if err := delConsumerScript.Run(ctx, b.client, []string{b.streamKey(q)}, group, c.Name).Err(); err != nil {
				return fmt.Errorf("redisqueue: maintain del consumer %q: %w", c.Name, err)
			}
		}
		empty, err := b.queueEmpty(ctx, q)
		if err != nil {
			return err
		}
		if empty {
			if err := b.client.SRem(ctx, b.queuesKey(), q).Err(); err != nil {
				return fmt.Errorf("redisqueue: maintain srem %q: %w", q, err)
			}
		}
	}
	return nil
}

func (b *Broker) queueEmpty(ctx context.Context, q string) (bool, error) {
	streamLen, err := b.client.XLen(ctx, b.streamKey(q)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return false, fmt.Errorf("redisqueue: maintain xlen %q: %w", q, err)
	}
	delayed, err := b.client.ZCard(ctx, b.delayedKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain zcard %q: %w", q, err)
	}
	dead, err := b.client.HLen(ctx, b.deadKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain hlen %q: %w", q, err)
	}
	poison, err := b.client.LLen(ctx, b.poisonKey(q)).Result()
	if err != nil {
		return false, fmt.Errorf("redisqueue: maintain llen %q: %w", q, err)
	}
	return streamLen == 0 && delayed == 0 && dead == 0 && poison == 0, nil
}
