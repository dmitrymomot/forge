package redisqueue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/core/id"
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

var purgeDeadBeforeScript = redis.NewScript(`
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for i = 1, #ids do
  redis.call('ZREM', KEYS[1], ids[i])
  redis.call('HDEL', KEYS[2], ids[i])
  redis.call('HDEL', KEYS[3], ids[i])
end
return #ids
`)

// Broker is the Redis queue.Broker.
type Broker struct {
	client   redis.UniversalClient
	claimed  map[string]claimedRef // job id → ref; only the claiming instance finalizes
	groups   map[string]bool       // queues with the consumer group ensured
	prefix   string
	consumer string

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

// New builds a Broker over client.
func New(client redis.UniversalClient, opts ...Option) (*Broker, error) {
	host, _ := os.Hostname()
	b := &Broker{
		client:   client,
		prefix:   "queue:",
		consumer: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), id.NewULID().String()),
		claimed:  make(map[string]claimedRef),
		groups:   make(map[string]bool),
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
		retries := make(map[string]int64, len(msgs))
		pend, err := b.client.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: b.streamKey(q), Group: group,
			Start: msgs[0].ID, End: msgs[len(msgs)-1].ID, Count: int64(len(msgs)),
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("redisqueue: pending: %w", err)
		}
		for _, p := range pend {
			retries[p.ID] = p.RetryCount
		}
		for _, m := range msgs {
			j, err := b.decodeMsg(m)
			if err != nil {
				return nil, err
			}
			delivered := retries[m.ID]
			if delivered == 0 {
				delivered = 1
			}
			claimedJob := j
			claimedJob.Attempt = j.Attempt + int(delivered)
			b.remember(claimedJob.ID, claimedRef{job: claimedJob, msgID: m.ID, queue: q, token: token})
			out = append(out, queue.ClaimedJob{Job: claimedJob, Token: token})
		}
		remaining -= len(msgs)
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
					return nil, err
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
	for _, q := range queues {
		// "(" makes the score bound exclusive: died_at < cutoff, not <=.
		n, err := purgeDeadBeforeScript.Run(ctx, b.client,
			[]string{b.deadIdxKey(q), b.deadKey(q), b.indexKey()},
			fmt.Sprintf("(%d", cutoff.UnixMilli())).Int()
		if err != nil {
			return total, fmt.Errorf("redisqueue: purge dead before %q: %w", q, err)
		}
		total += n
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
