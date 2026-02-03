package resend

// Config holds Resend email provider configuration.
// Embed this in your app config for env parsing with caarlos0/env.
type Config struct {
	APIKey      string `env:"RESEND_API_KEY"`
	SenderEmail string `env:"RESEND_FROM_EMAIL"`
	SenderName  string `env:"RESEND_FROM_NAME"`
}
