package dnsverify

import "net"

// Verifier performs single-shot DNS verification against a Resolver. Build it
// with New; it is safe for concurrent use.
type Verifier struct {
	resolver Resolver
	cfg      Config
}

// New builds a Verifier. It applies DefaultConfig, then the options, then
// returns an error if the resulting Config is invalid. The resolver defaults
// to net.DefaultResolver.
func New(opts ...Option) (*Verifier, error) {
	cf := config{cfg: DefaultConfig()}
	for _, o := range opts {
		o(&cf)
	}
	if err := cf.cfg.Validate(); err != nil {
		return nil, err
	}
	r := cf.resolver
	if r == nil {
		r = net.DefaultResolver
	}
	return &Verifier{resolver: r, cfg: cf.cfg}, nil
}
