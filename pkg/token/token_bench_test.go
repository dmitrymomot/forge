package token_test

import (
	"testing"

	"github.com/dmitrymomot/forge/pkg/token"
)

func BenchmarkGenerateToken(b *testing.B) {
	secret := []byte("bench-secret-key-32-bytes-long!!")

	b.Run("SmallPayload", func(b *testing.B) {
		payload := EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}

		b.ResetTimer()
		for b.Loop() {
			tok, err := token.GenerateToken(payload, secret)
			if err != nil {
				b.Fatal(err)
			}
			if tok == "" {
				b.Fatal("empty token")
			}
		}
	})

	b.Run("LargePayload", func(b *testing.B) {
		payload := map[string]any{
			"user_id":      "usr_123456789",
			"email":        "user@example.com",
			"workspace_id": "ws_987654321",
			"role":         "admin",
			"permissions":  []string{"read", "write", "delete", "manage"},
			"metadata": map[string]string{
				"source":   "invitation",
				"campaign": "onboarding-2024",
				"referrer": "https://example.com/invite",
			},
		}

		b.ResetTimer()
		for b.Loop() {
			tok, err := token.GenerateToken(payload, secret)
			if err != nil {
				b.Fatal(err)
			}
			if tok == "" {
				b.Fatal("empty token")
			}
		}
	})
}

func BenchmarkParseToken(b *testing.B) {
	secret := []byte("bench-secret-key-32-bytes-long!!")

	b.Run("SmallPayload", func(b *testing.B) {
		payload := EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}

		tok, err := token.GenerateToken(payload, secret)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for b.Loop() {
			result, err := token.ParseToken[EmailVerification](tok, secret)
			if err != nil {
				b.Fatal(err)
			}
			if result.UserID != payload.UserID {
				b.Fatal("user ID mismatch")
			}
		}
	})

	b.Run("LargePayload", func(b *testing.B) {
		type LargePayload struct {
			UserID      string            `json:"user_id"`
			Email       string            `json:"email"`
			WorkspaceID string            `json:"workspace_id"`
			Role        string            `json:"role"`
			Permissions []string          `json:"permissions"`
			Metadata    map[string]string `json:"metadata"`
		}

		payload := LargePayload{
			UserID:      "usr_123456789",
			Email:       "user@example.com",
			WorkspaceID: "ws_987654321",
			Role:        "admin",
			Permissions: []string{"read", "write", "delete", "manage"},
			Metadata: map[string]string{
				"source":   "invitation",
				"campaign": "onboarding-2024",
				"referrer": "https://example.com/invite",
			},
		}

		tok, err := token.GenerateToken(payload, secret)
		if err != nil {
			b.Fatal(err)
		}

		b.ResetTimer()
		for b.Loop() {
			result, err := token.ParseToken[LargePayload](tok, secret)
			if err != nil {
				b.Fatal(err)
			}
			if result.UserID != payload.UserID {
				b.Fatal("user ID mismatch")
			}
		}
	})
}

func BenchmarkRoundTrip(b *testing.B) {
	secret := []byte("bench-secret-key-32-bytes-long!!")

	b.Run("SmallPayload", func(b *testing.B) {
		payload := EmailVerification{
			UserID: "usr_abc123",
			Email:  "user@example.com",
			Action: "verify_email",
		}

		b.ResetTimer()
		for b.Loop() {
			tok, err := token.GenerateToken(payload, secret)
			if err != nil {
				b.Fatal(err)
			}

			result, err := token.ParseToken[EmailVerification](tok, secret)
			if err != nil {
				b.Fatal(err)
			}
			if result.UserID != payload.UserID {
				b.Fatal("user ID mismatch")
			}
		}
	})

	b.Run("LargePayload", func(b *testing.B) {
		type LargePayload struct {
			UserID      string            `json:"user_id"`
			Email       string            `json:"email"`
			WorkspaceID string            `json:"workspace_id"`
			Role        string            `json:"role"`
			Permissions []string          `json:"permissions"`
			Metadata    map[string]string `json:"metadata"`
		}

		payload := LargePayload{
			UserID:      "usr_123456789",
			Email:       "user@example.com",
			WorkspaceID: "ws_987654321",
			Role:        "admin",
			Permissions: []string{"read", "write", "delete", "manage"},
			Metadata: map[string]string{
				"source":   "invitation",
				"campaign": "onboarding-2024",
				"referrer": "https://example.com/invite",
			},
		}

		b.ResetTimer()
		for b.Loop() {
			tok, err := token.GenerateToken(payload, secret)
			if err != nil {
				b.Fatal(err)
			}

			result, err := token.ParseToken[LargePayload](tok, secret)
			if err != nil {
				b.Fatal(err)
			}
			if result.UserID != payload.UserID {
				b.Fatal("user ID mismatch")
			}
		}
	})
}
