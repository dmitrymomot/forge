// Package config layers application configuration from YAML (per-env convention
// with ${VAR:default} substitution), .env inheritance, and env-tagged structs.
//
// # Import alias required in packages that use the options idiom
//
// The package name "config" collides at compile time with the unexported
// package-level `type config struct` that forge's options idiom puts in nearly
// every package ("config already declared through import of package config").
// Any package that both uses that idiom and imports this loader must alias it:
//
//	import appconfig "github.com/dmitrymomot/forge/ops/config"
//
// # YAML per-env
//
//	// config/development.yaml:  host: ${HOST:0.0.0.0}
//	type Server struct {
//		Host string `yaml:"host"`
//		Port int    `yaml:"port"`
//	}
//	srv, err := config.Load[Server]("config/") // reads config/{APP_ENV}.yaml
//
// # .env inheritance (later files and the real env override earlier files)
//
//	err := config.Dotenv("config/.env.local", ".env")
//
// # Struct-from-env (12-factor, no YAML file)
//
//	type App struct {
//		Name string `env:"NAME,required"`
//		Port int    `env:"PORT,default=8080"`
//	}
//	app, err := config.LoadEnv[App](config.WithDotenv(".env"))
//
// ${VAR:default} uses the default when VAR is unset OR empty (shell :- form);
// ${VAR} with no default errors when unset. Profile() reads APP_ENV then ENV
// (default "development"); IsDev/IsProd/IsTest/IsStaging classify it.
package config
