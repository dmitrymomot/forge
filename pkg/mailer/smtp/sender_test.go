package smtp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/mailer"
)

func TestExtractEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		want    string
		wantErr bool
	}{
		{
			name: "bare email address",
			addr: "user@example.com",
			want: "user@example.com",
		},
		{
			name: "RFC 5322 format with name",
			addr: "John Doe <john@example.com>",
			want: "john@example.com",
		},
		{
			name: "RFC 5322 format with quoted name",
			addr: `"Jane Smith" <jane@example.com>`,
			want: "jane@example.com",
		},
		{
			name: "RFC 5322 format with special characters in name",
			addr: `"O'Brien, Patrick" <patrick@example.com>`,
			want: "patrick@example.com",
		},
		{
			name:    "invalid empty address",
			addr:    "",
			wantErr: true,
		},
		{
			name:    "invalid format - missing @",
			addr:    "notanemail",
			wantErr: true,
		},
		{
			name:    "invalid format - malformed brackets",
			addr:    "Name <invalid",
			wantErr: true,
		},
		{
			name:    "invalid format - brackets without email",
			addr:    "Name <>",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractEmail(tt.addr)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "parse address")
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCollectRecipients(t *testing.T) {
	t.Parallel()

	t.Run("combines To, CC, and BCC", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To:  []string{"user1@example.com", "User Two <user2@example.com>"},
			CC:  []string{"cc@example.com"},
			BCC: []string{"bcc1@example.com", "BCC User <bcc2@example.com>"},
		}

		got, err := collectRecipients(email)
		require.NoError(t, err)
		require.Len(t, got, 5)
		require.Equal(t, []string{
			"user1@example.com",
			"user2@example.com",
			"cc@example.com",
			"bcc1@example.com",
			"bcc2@example.com",
		}, got)
	})

	t.Run("handles only To recipients", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To: []string{"single@example.com"},
		}

		got, err := collectRecipients(email)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, []string{"single@example.com"}, got)
	})

	t.Run("extracts bare emails from RFC 5322 format", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To: []string{
				"Plain <plain@example.com>",
				`"Quoted Name" <quoted@example.com>`,
			},
			CC: []string{"bare@example.com"},
		}

		got, err := collectRecipients(email)
		require.NoError(t, err)
		require.Equal(t, []string{
			"plain@example.com",
			"quoted@example.com",
			"bare@example.com",
		}, got)
	})

	t.Run("returns error for invalid To address", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To: []string{"valid@example.com", "invalid"},
		}

		_, err := collectRecipients(email)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse address")
	})

	t.Run("returns error for invalid CC address", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To: []string{"valid@example.com"},
			CC: []string{"bad-cc"},
		}

		_, err := collectRecipients(email)
		require.Error(t, err)
	})

	t.Run("returns error for invalid BCC address", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{
			To:  []string{"valid@example.com"},
			BCC: []string{"malformed<"},
		}

		_, err := collectRecipients(email)
		require.Error(t, err)
	})

	t.Run("handles empty recipient lists", func(t *testing.T) {
		t.Parallel()

		email := &mailer.Email{}

		got, err := collectRecipients(email)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}
