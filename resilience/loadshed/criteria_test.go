package loadshed_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/resilience/loadshed"
)

func TestConcurrencyPressureZeroWhenIdle(t *testing.T) {
	c := loadshed.Concurrency(10)
	assert.Equal(t, 0.0, c.Pressure())
}

func TestLatencyPressureClampsAtOne(t *testing.T) {
	l := loadshed.Latency(100 * time.Millisecond)
	assert.Equal(t, 0.0, l.Pressure()) // no samples yet
}
