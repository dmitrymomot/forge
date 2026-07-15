package queue_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/async/queue"
)

type welcomePayload struct {
	UserID string `json:"user_id"`
}

func TestNewKind_Name(t *testing.T) {
	t.Parallel()
	k := queue.NewKind[welcomePayload]("email.send_welcome")
	assert.Equal(t, "email.send_welcome", k.Name())
}

func TestNewKind_PanicsOnEmptyName(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { queue.NewKind[welcomePayload]("") })
}
