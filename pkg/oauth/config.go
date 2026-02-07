package oauth

// GoogleConfig holds Google OAuth configuration.
type GoogleConfig struct {
	ClientID     string   `env:"GOOGLE_OAUTH_CLIENT_ID,required"`
	ClientSecret string   `env:"GOOGLE_OAUTH_CLIENT_SECRET,required"`
	RedirectURL  string   `env:"GOOGLE_OAUTH_REDIRECT_URL" envDefault:""`
	Scopes       []string `env:"GOOGLE_OAUTH_SCOPES" envSeparator:","`
}

// GitHubConfig holds GitHub OAuth configuration.
type GitHubConfig struct {
	ClientID     string   `env:"GITHUB_OAUTH_CLIENT_ID,required"`
	ClientSecret string   `env:"GITHUB_OAUTH_CLIENT_SECRET,required"`
	RedirectURL  string   `env:"GITHUB_OAUTH_REDIRECT_URL" envDefault:""`
	Scopes       []string `env:"GITHUB_OAUTH_SCOPES" envSeparator:","`
}
