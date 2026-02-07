package mailer

// Config holds mailer configuration.
// Embed this in your app config for env parsing with caarlos0/env.
type Config struct {
	FallbackSubject string `env:"FALLBACK_SUBJECT" envDefault:"Notification"`
	DefaultLayout   string `env:"DEFAULT_LAYOUT" envDefault:"base.html"`
}
