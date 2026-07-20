package webhook

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/crypto/consttime"
	"github.com/dmitrymomot/forge/crypto/sign"
)

// maxSignatureCandidates bounds how many v1 elements Stripe's Verify will
// consider; legitimate rotation never produces more than two.
const maxSignatureCandidates = 8

// Scheme is one HMAC signature wire format, shared by both directions: Sign
// produces the headers the Sender attaches to an outbound delivery, Verify
// authenticates an inbound request's payload against its headers in constant
// time. Register a bespoke partner scheme by implementing this interface —
// nothing else in the package needs forking. Implementations must be safe for
// concurrent use.
type Scheme interface {
	// Sign returns the signature headers for payload at time now.
	Sign(secret, payload []byte, now time.Time) (http.Header, error)

	// Verify authenticates payload against the request headers. Timestamped
	// schemes reject timestamps outside now±tolerance; tolerance <= 0
	// disables that check, and schemes without timestamps ignore both
	// arguments. Failures are ErrMissingSignature, ErrInvalidTimestamp, or
	// ErrInvalidSignature; an empty secret is reported as a wrapped
	// crypto/sign error.
	Verify(secret, payload []byte, header http.Header, now time.Time, tolerance time.Duration) error
}

// schemeConfig holds the header names a built-in scheme reads and writes.
type schemeConfig struct {
	sigHeader string
	tsHeader  string
}

// SchemeOption customizes a built-in scheme's header names.
type SchemeOption func(*schemeConfig)

// WithSignatureHeader overrides the signature header name. An empty name is
// ignored.
func WithSignatureHeader(name string) SchemeOption {
	return func(c *schemeConfig) {
		if name != "" {
			c.sigHeader = name
		}
	}
}

// WithTimestampHeader overrides the timestamp header name on schemes that
// carry the timestamp separately (Slack). An empty name is ignored; schemes
// with an embedded timestamp (Stripe) or none (GitHub) ignore it.
func WithTimestampHeader(name string) SchemeOption {
	return func(c *schemeConfig) {
		if name != "" {
			c.tsHeader = name
		}
	}
}

func newSchemeConfig(sigHeader, tsHeader string, opts []SchemeOption) schemeConfig {
	c := schemeConfig{sigHeader: sigHeader, tsHeader: tsHeader}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// newSigner wraps sign.New so an empty secret surfaces as a package-prefixed
// error instead of a bare crypto/sign sentinel.
func newSigner(secret []byte) (*sign.Signer, error) {
	s, err := sign.New(secret)
	if err != nil {
		return nil, fmt.Errorf("webhook: %w", err)
	}
	return s, nil
}

// checkTolerance rejects unix timestamps outside now±tolerance; tolerance <= 0
// disables the check.
func checkTolerance(ts int64, now time.Time, tolerance time.Duration) error {
	if tolerance <= 0 {
		return nil
	}
	d := now.Sub(time.Unix(ts, 0)).Abs()
	if d > tolerance {
		return fmt.Errorf("%w: timestamp %d outside tolerance %s", ErrInvalidTimestamp, ts, tolerance)
	}
	return nil
}

// Stripe returns the Stripe-style scheme: one header (default
// "Stripe-Signature") carrying "t=<unix>,v1=<hex>" where v1 is the HMAC-SHA256
// of "<t>.<payload>". Verify accepts multiple v1 elements (secret rotation on
// the sender's side) and ignores unknown elements. This is the Sender's
// default outbound scheme, renamed to "Webhook-Signature".
func Stripe(opts ...SchemeOption) Scheme {
	return stripeScheme{header: newSchemeConfig("Stripe-Signature", "", opts).sigHeader}
}

type stripeScheme struct {
	header string
}

// stripeMsg builds the signed payload "<ts>.<payload>". ts stays the exact
// string off the wire — re-formatting a parsed value would silently change
// the signed message for unusual-but-valid encodings (leading zeros).
func stripeMsg(ts string, payload []byte) []byte {
	msg := make([]byte, 0, len(ts)+1+len(payload))
	msg = append(msg, ts...)
	msg = append(msg, '.')
	msg = append(msg, payload...)
	return msg
}

func (s stripeScheme) Sign(secret, payload []byte, now time.Time) (http.Header, error) {
	signer, err := newSigner(secret)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := signer.Sign(stripeMsg(ts, payload))
	h := make(http.Header, 1)
	h.Set(s.header, "t="+ts+",v1="+hex.EncodeToString(mac))
	return h, nil
}

func (s stripeScheme) Verify(secret, payload []byte, header http.Header, now time.Time, tolerance time.Duration) error {
	val := header.Get(s.header)
	if val == "" {
		return fmt.Errorf("%w: %s", ErrMissingSignature, s.header)
	}
	signer, err := newSigner(secret)
	if err != nil {
		return err
	}
	var ts int64
	rawTS := ""
	tsSeen := false
	var macs [][]byte
	for elem := range strings.SplitSeq(val, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(elem), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return fmt.Errorf("%w: unparseable t element", ErrInvalidTimestamp)
			}
			ts, rawTS, tsSeen = n, v, true
		case "v1":
			// Cap candidates: rotation never yields more than two live
			// signatures, and the header is attacker-controlled.
			if len(macs) < maxSignatureCandidates {
				if mac, err := hex.DecodeString(v); err == nil {
					macs = append(macs, mac)
				}
			}
		}
	}
	if !tsSeen {
		return fmt.Errorf("%w: missing t element", ErrInvalidTimestamp)
	}
	if err := checkTolerance(ts, now, tolerance); err != nil {
		return err
	}
	// One full-message HMAC, however many v1 candidates arrived — recomputing
	// it per element would hand an unauthenticated caller a CPU amplifier.
	expected := signer.Sign(stripeMsg(rawTS, payload))
	for _, mac := range macs {
		if consttime.BytesEqual(expected, mac) {
			return nil
		}
	}
	return ErrInvalidSignature
}

// GitHub returns the GitHub-style scheme: one header (default
// "X-Hub-Signature-256") carrying "sha256=<hex>" — the HMAC-SHA256 of the raw
// payload. The format carries no timestamp, so Verify ignores tolerance;
// replay protection must come from the consumer (e.g. tracking GitHub's
// X-GitHub-Delivery header).
func GitHub(opts ...SchemeOption) Scheme {
	return githubScheme{header: newSchemeConfig("X-Hub-Signature-256", "", opts).sigHeader}
}

type githubScheme struct {
	header string
}

func (s githubScheme) Sign(secret, payload []byte, _ time.Time) (http.Header, error) {
	signer, err := newSigner(secret)
	if err != nil {
		return nil, err
	}
	h := make(http.Header, 1)
	h.Set(s.header, "sha256="+hex.EncodeToString(signer.Sign(payload)))
	return h, nil
}

func (s githubScheme) Verify(secret, payload []byte, header http.Header, _ time.Time, _ time.Duration) error {
	val := header.Get(s.header)
	if val == "" {
		return fmt.Errorf("%w: %s", ErrMissingSignature, s.header)
	}
	hexMac, ok := strings.CutPrefix(val, "sha256=")
	if !ok {
		return fmt.Errorf("%w: missing sha256= prefix", ErrInvalidSignature)
	}
	mac, err := hex.DecodeString(hexMac)
	if err != nil {
		return fmt.Errorf("%w: signature is not hex", ErrInvalidSignature)
	}
	signer, err := newSigner(secret)
	if err != nil {
		return err
	}
	if !signer.Verify(payload, mac) {
		return ErrInvalidSignature
	}
	return nil
}

// Slack returns the Slack-style scheme: a signature header (default
// "X-Slack-Signature") carrying "v0=<hex>" — the HMAC-SHA256 of
// "v0:<timestamp>:<payload>" — plus a separate timestamp header (default
// "X-Slack-Request-Timestamp").
func Slack(opts ...SchemeOption) Scheme {
	c := newSchemeConfig("X-Slack-Signature", "X-Slack-Request-Timestamp", opts)
	return slackScheme(c)
}

type slackScheme struct {
	sigHeader string
	tsHeader  string
}

// slackMsg builds the base string "v0:<ts>:<payload>".
func slackMsg(ts string, payload []byte) []byte {
	msg := make([]byte, 0, 3+len(ts)+1+len(payload))
	msg = append(msg, "v0:"...)
	msg = append(msg, ts...)
	msg = append(msg, ':')
	msg = append(msg, payload...)
	return msg
}

func (s slackScheme) Sign(secret, payload []byte, now time.Time) (http.Header, error) {
	signer, err := newSigner(secret)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	mac := signer.Sign(slackMsg(ts, payload))
	h := make(http.Header, 2)
	h.Set(s.sigHeader, "v0="+hex.EncodeToString(mac))
	h.Set(s.tsHeader, ts)
	return h, nil
}

func (s slackScheme) Verify(secret, payload []byte, header http.Header, now time.Time, tolerance time.Duration) error {
	val := header.Get(s.sigHeader)
	if val == "" {
		return fmt.Errorf("%w: %s", ErrMissingSignature, s.sigHeader)
	}
	tsStr := header.Get(s.tsHeader)
	if tsStr == "" {
		return fmt.Errorf("%w: %s", ErrMissingSignature, s.tsHeader)
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: unparseable %s", ErrInvalidTimestamp, s.tsHeader)
	}
	if err := checkTolerance(ts, now, tolerance); err != nil {
		return err
	}
	hexMac, ok := strings.CutPrefix(val, "v0=")
	if !ok {
		return fmt.Errorf("%w: missing v0= prefix", ErrInvalidSignature)
	}
	mac, err := hex.DecodeString(hexMac)
	if err != nil {
		return fmt.Errorf("%w: signature is not hex", ErrInvalidSignature)
	}
	signer, err := newSigner(secret)
	if err != nil {
		return err
	}
	if !signer.Verify(slackMsg(tsStr, payload), mac) {
		return ErrInvalidSignature
	}
	return nil
}
