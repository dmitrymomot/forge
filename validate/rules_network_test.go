package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/validate"
)

func TestNetwork(t *testing.T) {
	assert.True(t, validate.IP("192.168.0.1").IsZero())
	assert.True(t, validate.IP("2001:db8::1").IsZero())
	assert.Equal(t, "validation.ip", validate.IP("999.1.1.1").Key)

	assert.True(t, validate.IPv4("10.0.0.1").IsZero())
	assert.Equal(t, "validation.ipv4", validate.IPv4("2001:db8::1").Key)

	assert.True(t, validate.IPv6("2001:db8::1").IsZero())
	assert.Equal(t, "validation.ipv6", validate.IPv6("10.0.0.1").Key)

	assert.True(t, validate.MAC("01:23:45:67:89:ab").IsZero())
	assert.Equal(t, "validation.mac", validate.MAC("nope").Key)

	assert.True(t, validate.Domain("example.com").IsZero())
	assert.True(t, validate.Domain("sub.example.co.uk").IsZero())
	assert.Equal(t, "validation.domain", validate.Domain("-bad-.com").Key)
	assert.Equal(t, "validation.domain", validate.Domain("no_dot").Key)
}
