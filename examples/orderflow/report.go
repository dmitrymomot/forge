package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/dmitrymomot/forge/async/collector"
	"github.com/dmitrymomot/forge/async/queue"
	"github.com/dmitrymomot/forge/async/scheduler"
)

// reportPayload is the scheduler-enqueued job: scheduled work and ad-hoc work
// share the queue worker as one execution path.
type reportPayload struct {
	PeriodEnd time.Time `json:"period_end"`
}

var kindReport = queue.NewKind[reportPayload]("shop.report")

// registerReporting wires the periodic shop summary: the scheduler enqueues a
// shop.report job every 15 seconds, and the queue worker's handler logs order
// counts alongside queue and telemetry stats.
func registerReporting(sched *scheduler.Scheduler, worker *queue.Service, sh *shop, client *queue.Client, telemetry *collector.Collector[requestSeen], log *slog.Logger) {
	scheduler.AddFunc(sched, "shop.report", scheduler.Every(15*time.Second), kindReport,
		func(scheduledFor time.Time) (reportPayload, error) {
			return reportPayload{PeriodEnd: scheduledFor}, nil
		})

	queue.Register(worker, kindReport, func(ctx context.Context, p reportPayload) error {
		placed, fulfilled, failed := sh.counts()
		ts := telemetry.Stats()

		pending := 0
		if qs, err := client.Stats(ctx); err == nil {
			for _, q := range qs {
				pending += q.Pending
			}
		}

		log.Info("shop report",
			slog.Time("period_end", p.PeriodEnd),
			slog.Int("orders_in_flight", placed),
			slog.Int("orders_fulfilled", fulfilled),
			slog.Int("orders_failed", failed),
			slog.Int("jobs_pending", pending),
			slog.Uint64("telemetry_added", ts.Added),
			slog.Uint64("telemetry_flushed", ts.Flushed))
		return nil
	})
}
