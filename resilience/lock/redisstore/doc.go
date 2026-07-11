// Package redisstore implements lock.Store over a go-redis client, using
// SET NX PX for mutual exclusion, owner-checked Lua for refresh/release, and
// a companion INCR key for monotonic fencing tokens.
//
// It is NOT Redlock — multi-master/quorum locking is out of scope. For a
// single Redis instance it is correct and fast.
//
// # Usage
//
//	l := lock.New(redisstore.New(client, redisstore.WithPrefix("lock:")))
package redisstore
