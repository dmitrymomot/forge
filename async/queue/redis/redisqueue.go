package redisqueue

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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

func (b *Broker) Push(ctx context.Context, job queue.Job) error {
	enc, err := queue.EncodeJob(job)
	if err != nil {
		return err
	}
	if err := b.ensureGroup(ctx, job.Queue); err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.SAdd(ctx, b.queuesKey(), job.Queue)
	pipe.HSet(ctx, b.indexKey(), job.ID, job.Queue)
	if job.RunAt.After(time.Now()) {
		pipe.ZAdd(ctx, b.delayedKey(job.Queue), redis.Z{Score: float64(job.RunAt.UnixMilli()), Member: job.ID})
		pipe.HSet(ctx, b.dataKey(job.Queue), job.ID, enc)
	} else {
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: b.streamKey(job.Queue), Values: map[string]any{"j": enc}})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: push: %w", err)
	}
	return nil
}

func (b *Broker) Claim(ctx context.Context, q string, n int, lease time.Duration) ([]queue.Job, error) {
	if err := b.ensureGroup(ctx, q); err != nil {
		return nil, err
	}
	// Promote due delayed/retried jobs into the stream.
	nowMS := strconv.FormatInt(time.Now().UnixMilli(), 10)
	if err := promoteScript.Run(ctx, b.client, []string{b.delayedKey(q), b.dataKey(q), b.streamKey(q)}, nowMS, "128").Err(); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("redisqueue: promote delayed: %w", err)
	}

	remaining := n
	var out []queue.Job

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
			b.remember(claimedJob.ID, claimedRef{job: claimedJob, msgID: m.ID, queue: q})
			out = append(out, claimedJob)
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
				b.remember(claimedJob.ID, claimedRef{job: claimedJob, msgID: m.ID, queue: q})
				out = append(out, claimedJob)
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

func (b *Broker) take(id string) (claimedRef, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ref, ok := b.claimed[id]
	if ok {
		delete(b.claimed, id)
	}
	return ref, ok
}

func (b *Broker) Extend(ctx context.Context, jobID string, _ time.Duration) error {
	b.mu.Lock()
	ref, ok := b.claimed[jobID]
	b.mu.Unlock()
	if !ok {
		return queue.ErrJobNotFound
	}
	// JUSTID resets the idle clock without bumping the delivery counter.
	// Lease expiry is idle-based here: the next Claim's MinIdle is the lease.
	err := b.client.XClaimJustID(ctx, &redis.XClaimArgs{
		Stream: b.streamKey(ref.queue), Group: group, Consumer: b.consumer,
		MinIdle: 0, Messages: []string{ref.msgID},
	}).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("redisqueue: extend: %w", err)
	}
	return nil
}

func (b *Broker) Ack(ctx context.Context, jobID string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
	}
	pipe := b.client.TxPipeline()
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	pipe.HDel(ctx, b.indexKey(), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: ack: %w", err)
	}
	return nil
}

func (b *Broker) Nack(ctx context.Context, jobID string, retryAt time.Time, reason string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
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
	pipe := b.client.TxPipeline()
	pipe.ZAdd(ctx, b.delayedKey(ref.queue), redis.Z{Score: float64(retryAt.UnixMilli()), Member: jobID})
	pipe.HSet(ctx, b.dataKey(ref.queue), jobID, enc)
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: nack: %w", err)
	}
	return nil
}

func (b *Broker) Kill(ctx context.Context, jobID string, reason string) error {
	ref, ok := b.take(jobID)
	if !ok {
		return queue.ErrJobNotFound
	}
	j := ref.job // Attempt already reflects the consumed attempt (incl. crash redeliveries)
	j.LastError = reason
	enc, err := queue.EncodeJob(j)
	if err != nil {
		return err
	}
	pipe := b.client.TxPipeline()
	pipe.HSet(ctx, b.deadKey(ref.queue), jobID, enc)
	pipe.XAck(ctx, b.streamKey(ref.queue), group, ref.msgID)
	pipe.XDel(ctx, b.streamKey(ref.queue), ref.msgID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: kill: %w", err)
	}
	return nil
}

func (b *Broker) ListDead(ctx context.Context, q string, limit int) ([]queue.Job, error) {
	all, err := b.client.HGetAll(ctx, b.deadKey(q)).Result()
	if err != nil {
		return nil, fmt.Errorf("redisqueue: list dead: %w", err)
	}
	jobs := make([]queue.Job, 0, len(all))
	for _, enc := range all {
		j, err := queue.DecodeJob([]byte(enc))
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	sortDead(jobs)
	if len(jobs) > limit {
		jobs = jobs[:limit]
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
	pipe := b.client.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{Stream: b.streamKey(q), Values: map[string]any{"j": fresh}})
	pipe.HDel(ctx, b.deadKey(q), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: requeue: %w", err)
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
	exists, err := b.client.HExists(ctx, b.deadKey(q), jobID).Result()
	if err != nil {
		return fmt.Errorf("redisqueue: purge exists: %w", err)
	}
	if !exists {
		return queue.ErrNotDead
	}
	pipe := b.client.TxPipeline()
	pipe.HDel(ctx, b.deadKey(q), jobID)
	pipe.HDel(ctx, b.indexKey(), jobID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisqueue: purge: %w", err)
	}
	return nil
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

// sortDead orders dead jobs by CreatedAt then ID (HGETALL is unordered).
func sortDead(jobs []queue.Job) {
	slices.SortFunc(jobs, func(a, c queue.Job) int {
		if r := a.CreatedAt.Compare(c.CreatedAt); r != 0 {
			return r
		}
		return cmp.Compare(a.ID, c.ID)
	})
}
