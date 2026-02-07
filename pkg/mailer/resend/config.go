package resend

// Config holds Resend email provider configuration.
// Embed this in your app config for env parsing with caarlos0/env.
type Config struct {
	APIKey      string `env:"API_KEY,required"`
	SenderEmail string `env:"FROM_EMAIL"`
	SenderName  string `env:"FROM_NAME"`
}
