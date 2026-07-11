package mmdb

type source struct {
	path    string
	bytes   []byte
	hasPath bool
}

type config struct {
	city     source
	asn      source
	inMemory bool
}

// Option configures New and Reload.
type Option func(*config)

//nolint:unused // Used by New and Reload (implemented in future tasks).
func newConfig(opts ...Option) config {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c
}

// WithCity loads the city/country database from a file path. On unix the file
// is memory-mapped; elsewhere it is read into memory.
func WithCity(path string) Option {
	return func(c *config) { c.city = source{path: path, hasPath: true} }
}

// WithASN loads the ASN database from a file path (mmap on unix).
func WithASN(path string) Option {
	return func(c *config) { c.asn = source{path: path, hasPath: true} }
}

// WithCityBytes loads the city/country database from an in-memory byte slice
// (e.g. go:embed). The slice must not be modified after the call.
func WithCityBytes(b []byte) Option {
	return func(c *config) { c.city = source{bytes: b} }
}

// WithASNBytes loads the ASN database from an in-memory byte slice.
func WithASNBytes(b []byte) Option {
	return func(c *config) { c.asn = source{bytes: b} }
}

// WithInMemory forces file-path databases to be read into the heap instead of
// memory-mapped.
func WithInMemory() Option {
	return func(c *config) { c.inMemory = true }
}

// loadSource turns a source into its bytes plus a closer. Byte sources return a
// no-op closer; file sources mmap (or read, when inMemory) and their closer
// unmaps. A source with neither path nor bytes returns ErrNoDatabase.
func loadSource(src source, inMemory bool) (data []byte, closer func() error, err error) {
	switch {
	case src.hasPath && !inMemory:
		return mapFile(src.path)
	case src.hasPath:
		data, err = readFile(src.path)
		return data, func() error { return nil }, err
	case src.bytes != nil:
		return src.bytes, func() error { return nil }, nil
	default:
		return nil, nil, ErrNoDatabase
	}
}
