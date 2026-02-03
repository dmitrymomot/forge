package job

import (
	"context"
)

// scheduledHandler wraps a scheduled task's Handle method.
type scheduledHandler func(ctx context.Context) error

// scheduleConfig holds configuration for a scheduled task.
type scheduleConfig struct {
	handler  scheduledHandler
	name     string
	schedule string
}
