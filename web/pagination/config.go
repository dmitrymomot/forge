package pagination

const (
	defaultLimit int32 = 20
	defaultMax   int32 = 100
)

type config struct {
	pageParam        string
	perPageParam     string
	cursorParam      string
	cursorLimitParam string
	limit            int32
	max              int32
}

func defaultConfig() config {
	return config{
		pageParam:        "page",
		perPageParam:     "per_page",
		cursorParam:      "cursor",
		cursorLimitParam: "limit",
		limit:            defaultLimit,
		max:              defaultMax,
	}
}

func newConfig(opts []Option) config {
	c := defaultConfig()
	for _, o := range opts {
		o(&c)
	}
	if c.limit > c.max {
		c.limit = c.max
	}
	return c
}
