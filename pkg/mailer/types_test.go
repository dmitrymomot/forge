package mailer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSimpleTags_CreatesPresenceOnlyTags(t *testing.T) {
	t.Parallel()

	tags := SimpleTags("welcome", "onboarding", "transactional")

	require.Len(t, tags, 3)
	require.Contains(t, tags, "welcome")
	require.Contains(t, tags, "onboarding")
	require.Contains(t, tags, "transactional")

	// Verify values are empty structs (presence-only)
	require.Equal(t, struct{}{}, tags["welcome"])
	require.Equal(t, struct{}{}, tags["onboarding"])
	require.Equal(t, struct{}{}, tags["transactional"])
}

func TestSimpleTags_EmptyList(t *testing.T) {
	t.Parallel()

	tags := SimpleTags()

	require.NotNil(t, tags)
	require.Empty(t, tags)
}

func TestSimpleTags_SingleTag(t *testing.T) {
	t.Parallel()

	tags := SimpleTags("newsletter")

	require.Len(t, tags, 1)
	require.Contains(t, tags, "newsletter")
	require.Equal(t, struct{}{}, tags["newsletter"])
}

func TestRecipient_WithName(t *testing.T) {
	t.Parallel()

	result := Recipient("John Doe", "john@example.com")

	require.Equal(t, "John Doe <john@example.com>", result)
}

func TestRecipient_WithoutName(t *testing.T) {
	t.Parallel()

	result := Recipient("", "john@example.com")

	require.Equal(t, "john@example.com", result)
}

func TestRecipient_EmptyName(t *testing.T) {
	t.Parallel()

	result := Recipient("   ", "john@example.com")

	// Function doesn't trim, returns format as-is with spaces
	require.Equal(t, "    <john@example.com>", result)
}

func TestTags_CanHoldKeyValuePairs(t *testing.T) {
	t.Parallel()

	tags := make(Tags)
	tags["campaign"] = "summer-2024"
	tags["priority"] = "high"
	tags["user_type"] = "premium"

	require.Len(t, tags, 3)
	require.Equal(t, "summer-2024", tags["campaign"])
	require.Equal(t, "high", tags["priority"])
	require.Equal(t, "premium", tags["user_type"])
}

func TestTags_CanMixPresenceAndKeyValue(t *testing.T) {
	t.Parallel()

	tags := SimpleTags("newsletter", "automated")
	tags["campaign"] = "holiday-2024"
	tags["priority"] = 1

	require.Len(t, tags, 4)
	require.Equal(t, struct{}{}, tags["newsletter"])
	require.Equal(t, struct{}{}, tags["automated"])
	require.Equal(t, "holiday-2024", tags["campaign"])
	require.Equal(t, 1, tags["priority"])
}

func TestEmail_Structure(t *testing.T) {
	t.Parallel()

	// Verify Email struct can hold all expected fields
	email := &Email{
		To:      []string{"user@example.com"},
		CC:      []string{"manager@example.com"},
		BCC:     []string{"archive@example.com"},
		From:    "sender@example.com",
		ReplyTo: "support@example.com",
		Subject: "Test Email",
		HTML:    "<p>Hello</p>",
		Text:    "Hello",
		Headers: map[string]string{"X-Custom": "value"},
		Tags:    SimpleTags("test"),
		Attachments: []Attachment{
			{
				Filename:    "doc.pdf",
				ContentType: "application/pdf",
				Content:     []byte("content"),
				ContentID:   "cid:doc",
			},
		},
	}

	require.NotNil(t, email)
	require.Equal(t, "user@example.com", email.To[0])
	require.Equal(t, "manager@example.com", email.CC[0])
	require.Equal(t, "archive@example.com", email.BCC[0])
	require.Equal(t, "sender@example.com", email.From)
	require.Equal(t, "support@example.com", email.ReplyTo)
	require.Equal(t, "Test Email", email.Subject)
	require.Equal(t, "<p>Hello</p>", email.HTML)
	require.Equal(t, "Hello", email.Text)
	require.Equal(t, "value", email.Headers["X-Custom"])
	require.Contains(t, email.Tags, "test")
	require.Len(t, email.Attachments, 1)
	require.Equal(t, "doc.pdf", email.Attachments[0].Filename)
}

func TestAttachment_Structure(t *testing.T) {
	t.Parallel()

	attachment := Attachment{
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		ContentID:   "cid:report",
		Content:     []byte("PDF content here"),
	}

	require.Equal(t, "report.pdf", attachment.Filename)
	require.Equal(t, "application/pdf", attachment.ContentType)
	require.Equal(t, "cid:report", attachment.ContentID)
	require.Equal(t, []byte("PDF content here"), attachment.Content)
}
