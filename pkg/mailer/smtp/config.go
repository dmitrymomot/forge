package smtp

// Config holds SMTP email provider configuration.
// Defaults to Mailpit (localhost:1025, no auth, no TLS) for local development.
// Embed this in your app config for env parsing with caarlos0/env.
type Config struct {
	Host        string `env:"HOST"       envDefault:"localhost"`
	Username    string `env:"USERNAME"`
	Password    string `env:"PASSWORD"`
	SenderEmail string `env:"FROM_EMAIL"`
	SenderName  string `env:"FROM_NAME"`
	Port        int    `env:"PORT"       envDefault:"1025"`
	TLS         bool   `env:"TLS"        envDefault:"false"`
}
