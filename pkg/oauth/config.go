package oauth

// GoogleConfig holds Google OAuth configuration.
type GoogleConfig struct {
	ClientID     string   `env:"CLIENT_ID,required"`
	ClientSecret string   `env:"CLIENT_SECRET,required"`
	RedirectURL  string   `env:"REDIRECT_URL"`
	Scopes       []string `env:"SCOPES" envSeparator:","`
}

// GitHubConfig holds GitHub OAuth configuration.
type GitHubConfig struct {
	ClientID     string   `env:"CLIENT_ID,required"`
	ClientSecret string   `env:"CLIENT_SECRET,required"`
	RedirectURL  string   `env:"REDIRECT_URL"`
	Scopes       []string `env:"SCOPES" envSeparator:","`
}
