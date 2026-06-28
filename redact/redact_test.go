package redact_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/redact"
)

func TestSecret_Expose(t *testing.T) {
	s := redact.New("super-secret")
	assert.Equal(t, "super-secret", s.Expose())
}

func TestSecret_StringMasks(t *testing.T) {
	s := redact.New("super-secret")
	assert.Equal(t, "REDACTED", s.String())
	assert.Equal(t, "REDACTED", fmt.Sprintf("%v", s))
	assert.Equal(t, "REDACTED", fmt.Sprintf("%s", s))  //nolint:staticcheck // exercises the fmt %s Stringer path explicitly
	assert.Equal(t, "REDACTED", fmt.Sprintf("%#v", s)) // GoString
}

func TestSecret_JSONMasks(t *testing.T) {
	type cfg struct {
		Name string                `json:"name"`
		Key  redact.Secret[string] `json:"key"`
	}
	out, err := json.Marshal(cfg{Name: "app", Key: redact.New("sk_live_123")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"app","key":"REDACTED"}`, string(out))
}

func TestSecret_LogValueMasks(t *testing.T) {
	s := redact.New([]byte("bytes-secret"))
	assert.Equal(t, "REDACTED", s.LogValue().String())
}

func TestString(t *testing.T) {
	assert.Equal(t, "sk_l***f8a2", redact.String("sk_live_abcdef8a2"[:7]+"f8a2")) // 11-char input
	assert.Equal(t, "REDACTED", redact.String("short"))                           // too short to mask safely
	assert.Equal(t, "REDACTED", redact.String("12345678"))                        // == 2*keep, still masked whole
}

func TestString_Unicode(t *testing.T) {
	// Multi-byte UTF-8 must mask on rune boundaries, not byte boundaries.
	assert.Equal(t, "一二三四***七八九十", redact.String("一二三四五六七八九十"))
}

func TestMap_ScrubsWithoutMutating(t *testing.T) {
	in := map[string]any{"user": "ada", "password": "hunter2", "token": "t_abc"}
	out := redact.Map(in, "password", "token", "absent")
	assert.Equal(t, "REDACTED", out["password"])
	assert.Equal(t, "REDACTED", out["token"])
	assert.Equal(t, "ada", out["user"])
	// original is untouched
	assert.Equal(t, "hunter2", in["password"])
	_, hasAbsent := out["absent"]
	assert.False(t, hasAbsent)
}
