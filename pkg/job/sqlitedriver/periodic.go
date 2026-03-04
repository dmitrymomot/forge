package sqlitedriver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/dmitrymomot/forge/pkg/job"
)

// periodicEntry holds a parsed cron schedule and its associated task.
type periodicEntry struct {
	lastRun  time.Time
	schedule cron.Schedule
	taskName string
}

// parsePeriodicJobs validates all cron expressions up front and returns
// the parsed entries. Returns an error if any expression is invalid.
func parsePeriodicJobs(jobs []job.PeriodicJobConfig) ([]periodicEntry, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	entries := make([]periodicEntry, 0, len(jobs))
	for _, pj := range jobs {
		schedule, err := parser.Parse(pj.Schedule)
		if err != nil {
			return nil, fmt.Errorf("sqlitedriver: invalid cron schedule %q: %w", pj.Schedule, err)
		}
		entries = append(entries, periodicEntry{
			taskName: pj.TaskName,
			schedule: schedule,
			lastRun:  time.Now().UTC(),
		})
	}
	return entries, nil
}

// runPeriodicScheduler evaluates cron schedules on each tick and inserts
// pending jobs when they're due. Uses dedup via unique_key to prevent
// double-inserts within a 1-minute window.
func (d *SQLiteDriver) runPeriodicScheduler(ctx context.Context, entries []periodicEntry, logger *slog.Logger) {
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			now = now.UTC()
			for i := range entries {
				entry := &entries[i]
				next := entry.schedule.Next(entry.lastRun)
				if !next.After(now) {
					// Time to fire this periodic job.
					ji := &job.JobInsert{
						TaskName:  entry.taskName,
						UniqueKey: "periodic:" + entry.taskName,
						UniqueFor: 1 * time.Minute,
					}
					if err := insertJob(ctx, d.db, ji); err != nil {
						logger.ErrorContext(ctx, "periodic insert failed",
							slog.String("task", entry.taskName),
							slog.Any("error", err),
						)
						continue
					}
					entry.lastRun = now
					logger.DebugContext(ctx, "periodic job inserted",
						slog.String("task", entry.taskName),
					)
				}
			}
		}
	}
}
