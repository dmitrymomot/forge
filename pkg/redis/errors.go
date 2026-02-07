package redis

import "errors"

var (
	ErrEmptyConnectionURL = errors.New("redis: empty connection URL")
	ErrFailedToParseURL   = errors.New("redis: failed to parse connection URL")
	ErrConnectionFailed   = errors.New("redis: failed to establish connection")
	ErrHealthcheckFailed  = errors.New("redis: healthcheck failed")
)
