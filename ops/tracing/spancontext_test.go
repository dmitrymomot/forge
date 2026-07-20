package tracing_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/ops/tracing"
)

const (
	sampleTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sampleTraceHex    = "4bf92f3577b34da6a3ce929d0e0e4736"
	sampleSpanHex     = "00f067aa0ba902b7"
)

func TestParseTraceparentValid(t *testing.T) {
	sc, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	assert.True(t, sc.IsValid())
	assert.Equal(t, sampleTraceHex, sc.TraceID.String())
	assert.Equal(t, sampleSpanHex, sc.SpanID.String())
	assert.True(t, sc.Sampled)
	assert.Empty(t, sc.TraceState)
}

func TestParseTraceparentNotSampled(t *testing.T) {
	sc, err := tracing.ParseTraceparent("00-" + sampleTraceHex + "-" + sampleSpanHex + "-00")
	require.NoError(t, err)
	assert.False(t, sc.Sampled)
}

func TestParseTraceparentOnlySampledBitRead(t *testing.T) {
	sc, err := tracing.ParseTraceparent("00-" + sampleTraceHex + "-" + sampleSpanHex + "-fd")
	require.NoError(t, err)
	assert.True(t, sc.Sampled)

	sc, err = tracing.ParseTraceparent("00-" + sampleTraceHex + "-" + sampleSpanHex + "-fe")
	require.NoError(t, err)
	assert.False(t, sc.Sampled)
}

func TestParseTraceparentFutureVersion(t *testing.T) {
	// A future version may append extra dash-separated fields.
	sc, err := tracing.ParseTraceparent("cc-" + sampleTraceHex + "-" + sampleSpanHex + "-01-extra-stuff")
	require.NoError(t, err)
	assert.Equal(t, sampleTraceHex, sc.TraceID.String())

	sc, err = tracing.ParseTraceparent("cc-" + sampleTraceHex + "-" + sampleSpanHex + "-01")
	require.NoError(t, err)
	assert.True(t, sc.Sampled)
}

func TestParseTraceparentInvalid(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"too short":          "00-abc-def-01",
		"wrong separator":    "00_" + sampleTraceHex + "_" + sampleSpanHex + "_01",
		"uppercase trace id": "00-" + strings.ToUpper(sampleTraceHex) + "-" + sampleSpanHex + "-01",
		"uppercase span id":  "00-" + sampleTraceHex + "-" + strings.ToUpper(sampleSpanHex) + "-01",
		"non-hex trace id":   "00-" + strings.Repeat("zz", 16) + "-" + sampleSpanHex + "-01",
		"zero trace id":      "00-" + strings.Repeat("0", 32) + "-" + sampleSpanHex + "-01",
		"zero span id":       "00-" + sampleTraceHex + "-" + strings.Repeat("0", 16) + "-01",
		"version ff":         "ff-" + sampleTraceHex + "-" + sampleSpanHex + "-01",
		"bad version":        "0x-" + sampleTraceHex + "-" + sampleSpanHex + "-01",
		"bad flags":          "00-" + sampleTraceHex + "-" + sampleSpanHex + "-0x",
		"v00 trailing data":  sampleTraceparent + "-extra",
		"v00 trailing junk":  sampleTraceparent + "x",
		"future no dash":     "cc-" + sampleTraceHex + "-" + sampleSpanHex + "-01x",
		"missing flags":      "00-" + sampleTraceHex + "-" + sampleSpanHex,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tracing.ParseTraceparent(in)
			require.ErrorIs(t, err, tracing.ErrInvalidTraceparent)
		})
	}
}

func TestTraceparentRoundTrip(t *testing.T) {
	sc, err := tracing.ParseTraceparent(sampleTraceparent)
	require.NoError(t, err)
	assert.Equal(t, sampleTraceparent, sc.Traceparent())

	sc.Sampled = false
	out, err := tracing.ParseTraceparent(sc.Traceparent())
	require.NoError(t, err)
	assert.Equal(t, sc, out)
}

func TestTraceparentInvalidContextEmpty(t *testing.T) {
	assert.Empty(t, tracing.SpanContext{}.Traceparent())
	assert.Empty(t, tracing.SpanContext{TraceID: tracing.TraceID{1}}.Traceparent())
	assert.Empty(t, tracing.SpanContext{SpanID: tracing.SpanID{1}}.Traceparent())
}

func TestIDValidityAndString(t *testing.T) {
	assert.False(t, tracing.TraceID{}.IsValid())
	assert.False(t, tracing.SpanID{}.IsValid())
	assert.Equal(t, strings.Repeat("0", 32), tracing.TraceID{}.String())

	id := tracing.TraceID{0x01, 0xab}
	assert.True(t, id.IsValid())
	assert.Equal(t, "01ab"+strings.Repeat("0", 28), id.String())

	sid := tracing.SpanID{0xff}
	assert.True(t, sid.IsValid())
	assert.Equal(t, "ff"+strings.Repeat("0", 14), sid.String())
}
