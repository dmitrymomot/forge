package config

import "errors"

var (
	// ErrProfileFile is returned by Load when the {profile}.yaml file is missing or unreadable.
	ErrProfileFile = errors.New("config: profile file")
	// ErrSubstitute is returned when a ${VAR} placeholder is malformed or an unset no-default var is referenced.
	ErrSubstitute = errors.New("config: substitution")
	// ErrYAML is returned when the substituted YAML fails to unmarshal.
	ErrYAML = errors.New("config: yaml")
	// ErrDotenv is returned when a .env file cannot be read or applied.
	ErrDotenv = errors.New("config: dotenv")
	// ErrRequiredMissing is returned by LoadEnv/Populate when a required env key has no value.
	ErrRequiredMissing = errors.New("config: required env missing")
	// ErrParse is returned by LoadEnv/Populate when a value cannot be parsed into its field.
	ErrParse = errors.New("config: parse")
)
