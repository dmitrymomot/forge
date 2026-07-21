package auditlog

import (
	"context"

	"github.com/dmitrymomot/forge/core/clock"
)

type config struct {
	scope func(context.Context) (string, error)
	clock clock.Clock
	chain bool
}

func defaultConfig() config {
	return config{clock: clock.System()}
}
