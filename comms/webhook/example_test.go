package webhook_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/dmitrymomot/forge/comms/webhook"
)

func ExampleScheme() {
	// One Scheme serves both directions: sign an outbound payload, verify it
	// back on the inbound side.
	scheme := webhook.GitHub()
	secret := []byte("It's a Secret to Everybody")
	payload := []byte("Hello, World!")

	header, _ := scheme.Sign(secret, payload, time.Now())
	fmt.Println(header.Get("X-Hub-Signature-256"))

	err := scheme.Verify(secret, payload, header, time.Now(), 0)
	fmt.Println(err)

	err = scheme.Verify(secret, []byte("tampered"), header, time.Now(), 0)
	fmt.Println(errors.Is(err, webhook.ErrInvalidSignature))

	// Output:
	// sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17
	// <nil>
	// true
}

func ExampleVerify() {
	// Inbound: wrap the handler; the body arrives verified and intact.
	scheme := webhook.Stripe()
	secret := []byte("whsec_partner_secret")
	handler := webhook.Verify(scheme, webhook.StaticSecrets(secret))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, "received")
		}),
	)

	payload := `{"type":"invoice.paid"}`
	sig, _ := scheme.Sign(secret, []byte(payload), time.Now())
	req := httptest.NewRequest(http.MethodPost, "/hooks/stripe", strings.NewReader(payload))
	req.Header = sig

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())

	// An unsigned request never reaches the handler.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hooks/stripe", strings.NewReader(payload)))
	fmt.Println(rec.Code)

	// Output:
	// 200 received
	// 401
}
