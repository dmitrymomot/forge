package internal

import (
	"fmt"

	env "github.com/caarlos0/env/v11"
	_ "github.com/joho/godotenv/autoload"
)

// LoadConfig parses environment variables into dst using struct tags.
// It loads .env from the working directory automatically via godotenv.
// Struct fields use `env:"KEY"`, `envDefault:"value"`, and `envSeparator:","`
// tags to declare their bindings.
func LoadConfig(dst any) error {
	if err := env.Parse(dst); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}
