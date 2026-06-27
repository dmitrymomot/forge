package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"slices"
	"strings"

	osgo "github.com/opensearch-project/opensearch-go/v4"
	osapi "github.com/opensearch-project/opensearch-go/v4/opensearchapi"
)

const (
	indexSuffix    = ".index.json"
	templateSuffix = ".template.json"
)

// indexDef is a parsed <name>.index.json setup file.
type indexDef struct {
	name string
	body []byte
}

// templateDef is a parsed <name>.template.json setup file.
type templateDef struct {
	name string
	body []byte
}

// Setup applies declarative OpenSearch index and template definitions embedded in
// an fs.FS at boot. It is forward-only: it creates absent indices, PUT-upserts
// templates, and (with WithUpdateMappings) PUTs additive mappings onto existing
// indices. It mirrors the migration package's up-only stance.
type Setup struct {
	fsys           fs.FS
	updateMappings bool
}

// SetupOption configures a Setup.
type SetupOption func(*Setup)

// WithUpdateMappings, when enabled, makes Apply additionally PUT the mappings block
// of each <name>.index.json onto an already-existing index (additive field changes
// only; OpenSearch rejects non-additive changes, which remain a consumer-driven
// reindex). Default false.
func WithUpdateMappings(enabled bool) SetupOption {
	return func(s *Setup) { s.updateMappings = enabled }
}

// NewSetup builds a Setup over fsys. Definition files live at the root of fsys and
// are matched by suffix: <name>.index.json and <name>.template.json. Parsing is
// deferred to Apply so construction never fails.
func NewSetup(fsys fs.FS, opts ...SetupOption) *Setup {
	s := &Setup{fsys: fsys}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Apply provisions the parsed definitions against c. It first parses the FS
// (returning ErrSetup on a malformed file before touching the network), then for
// each template PUT-upserts it, and for each index creates it when absent (and, if
// WithUpdateMappings is set, PUTs its mappings when already present). It is
// idempotent: a second Apply with no FS changes performs no mutating index create.
func (s *Setup) Apply(ctx context.Context, c *osgo.Client) error {
	indices, templates, err := parseSetupFS(s.fsys)
	if err != nil {
		return err
	}

	api := apiFor(c)

	// Templates first (an index may rely on a matching template at create time).
	for _, t := range templates {
		if _, err := api.IndexTemplate.Create(ctx, osapi.IndexTemplateCreateReq{
			IndexTemplate: t.name,
			Body:          bytes.NewReader(t.body),
		}); err != nil {
			return fmt.Errorf("%w: template %q: %v", ErrSetup, t.name, err)
		}
	}

	for _, idx := range indices {
		exists, err := indexExists(ctx, api, idx.name)
		if err != nil {
			return fmt.Errorf("%w: index %q exists check: %v", ErrSetup, idx.name, err)
		}
		if !exists {
			if _, err := api.Indices.Create(ctx, osapi.IndicesCreateReq{
				Index: idx.name,
				Body:  bytes.NewReader(idx.body),
			}); err != nil {
				return fmt.Errorf("%w: create index %q: %v", ErrSetup, idx.name, err)
			}
			continue
		}
		if s.updateMappings {
			mapping, err := extractMappings(idx.body)
			if err != nil {
				return fmt.Errorf("%w: index %q mappings: %v", ErrSetup, idx.name, err)
			}
			if mapping != nil {
				if _, err := api.Indices.Mapping.Put(ctx, osapi.MappingPutReq{
					Indices: []string{idx.name},
					Body:    bytes.NewReader(mapping),
				}); err != nil {
					return fmt.Errorf("%w: update mappings %q: %v", ErrSetup, idx.name, err)
				}
			}
		}
	}
	return nil
}

// indexExists reports whether an index is present. The HEAD-based Exists call
// returns a 404 *opensearch.Response (and a non-typed error) when absent; presence
// is determined from the status code, not from the error.
func indexExists(ctx context.Context, api *osapi.Client, name string) (bool, error) {
	resp, err := api.Indices.Exists(ctx, osapi.IndicesExistsReq{Indices: []string{name}})
	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if resp == nil {
		return false, errors.New("opensearch: index existence check returned no response and no error")
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// extractMappings pulls the "mappings" object out of an index body for an additive
// mapping PUT. It returns nil (no error) when the index body has no mappings block.
func extractMappings(indexBody []byte) ([]byte, error) {
	var doc struct {
		Mappings json.RawMessage `json:"mappings"`
	}
	if err := json.Unmarshal(indexBody, &doc); err != nil {
		return nil, err
	}
	if len(doc.Mappings) == 0 {
		return nil, nil
	}
	return doc.Mappings, nil
}

// parseSetupFS reads every <name>.index.json and <name>.template.json at the root
// of fsys into sorted, validated definitions. It is a pure function (no network,
// no client) so it is unit-testable with fstest.MapFS. A nil fsys, an unreadable
// file, or malformed JSON is an ErrSetup-wrapped error.
func parseSetupFS(fsys fs.FS) ([]indexDef, []templateDef, error) {
	if fsys == nil {
		return nil, nil, fmt.Errorf("%w: nil fs.FS", ErrSetup)
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read dir: %v", ErrSetup, err)
	}

	var (
		indices   []indexDef
		templates []templateDef
		errs      []error
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case strings.HasSuffix(name, indexSuffix):
			body, perr := readJSON(fsys, name)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			indices = append(indices, indexDef{name: strings.TrimSuffix(name, indexSuffix), body: body})
		case strings.HasSuffix(name, templateSuffix):
			body, perr := readJSON(fsys, name)
			if perr != nil {
				errs = append(errs, perr)
				continue
			}
			templates = append(templates, templateDef{name: strings.TrimSuffix(name, templateSuffix), body: body})
		}
	}
	if len(errs) > 0 {
		return nil, nil, fmt.Errorf("%w: %v", ErrSetup, errors.Join(errs...))
	}

	// Deterministic order so Apply is reproducible.
	slices.SortFunc(indices, func(a, b indexDef) int { return strings.Compare(a.name, b.name) })
	slices.SortFunc(templates, func(a, b templateDef) int { return strings.Compare(a.name, b.name) })
	return indices, templates, nil
}

// readJSON reads a file and validates it parses as JSON, returning the raw bytes.
func readJSON(fsys fs.FS, name string) ([]byte, error) {
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", name, err)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%q is not valid JSON", name)
	}
	return body, nil
}
