package password

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/dmitrymomot/forge/kdf"
)

// Algorithm selects the hashing scheme.
type Algorithm int

const (
	// Argon2id is the default, recommended algorithm.
	Argon2id Algorithm = iota
	// Bcrypt is provided as a fallback / migration source.
	Bcrypt
)

type config struct {
	argon kdf.Params
	algo  Algorithm
	bcost int
}

// Option configures Hash.
type Option func(*config)

func newConfig(opts ...Option) *config {
	c := &config{algo: Argon2id, argon: kdf.DefaultParams(), bcost: bcrypt.DefaultCost}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithAlgorithm selects the hashing algorithm (default Argon2id).
func WithAlgorithm(a Algorithm) Option { return func(c *config) { c.algo = a } }

// WithArgon2Params overrides the Argon2id cost parameters.
func WithArgon2Params(p kdf.Params) Option { return func(c *config) { c.argon = p } }

// WithBcryptCost overrides the bcrypt cost (used only with Algorithm Bcrypt).
func WithBcryptCost(cost int) Option { return func(c *config) { c.bcost = cost } }
