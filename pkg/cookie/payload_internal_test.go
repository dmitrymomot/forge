package cookie

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUnframePayloadVersionMismatch verifies that a payload carrying an
// unsupported version byte is rejected with ErrBadVersion (not the misleading
// ErrBadSig). The check runs after a valid signature/decryption, so it is
// exercised here directly against the framing helpers.
func TestUnframePayloadVersionMismatch(t *testing.T) {
	t.Parallel()

	payload := framePayload([]byte("value"), 0)

	// Flip the version byte to an unsupported value.
	payload[0] = payloadVersion + 1

	_, err := unframePayload(payload)
	require.ErrorIs(t, err, ErrBadVersion)
	require.NotErrorIs(t, err, ErrBadSig)
}

// TestUnframePayloadTooShort verifies that a truncated payload (shorter than the
// header) is rejected with ErrBadSig, distinct from the version-mismatch path.
func TestUnframePayloadTooShort(t *testing.T) {
	t.Parallel()

	_, err := unframePayload([]byte{payloadVersion})
	require.ErrorIs(t, err, ErrBadSig)
}

// TestUnframePayloadRoundTrip confirms a freshly framed payload (current
// version) round-trips back to its value.
func TestUnframePayloadRoundTrip(t *testing.T) {
	t.Parallel()

	value, err := unframePayload(framePayload([]byte("hello"), 0))
	require.NoError(t, err)
	require.Equal(t, "hello", string(value))
}
