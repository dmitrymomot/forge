package redisbus

import "errors"

var (
	// ErrInvalidTopic is returned by Publish for a topic containing NUL, the
	// frame separator.
	ErrInvalidTopic = errors.New("redisbus: invalid topic")
	// ErrPublish wraps a failed Redis PUBLISH round trip.
	ErrPublish = errors.New("redisbus: publish failed")
)
