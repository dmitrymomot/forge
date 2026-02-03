package mailer

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Template represents an email template with metadata and body.
type Template struct {
	Metadata map[string]any
	Body     string
}

// ParseTemplate parses a template file content and extracts front matter metadata and markdown body.
func ParseTemplate(content []byte) (*Template, error) {
	delimiter := []byte("---")

	if !bytes.HasPrefix(content, delimiter) {
		return &Template{
			Metadata: make(map[string]any),
			Body:     string(content),
		}, nil
	}

	afterFirst := bytes.TrimPrefix(content, delimiter)
	afterFirst = bytes.TrimLeft(afterFirst, "\n\r")

	if len(afterFirst) == 0 {
		return nil, fmt.Errorf("%w: no content after opening delimiter", ErrInvalidFrontmatter)
	}

	endIdx := bytes.Index(afterFirst, delimiter)
	if endIdx == -1 {
		return nil, fmt.Errorf("%w: closing delimiter not found", ErrInvalidFrontmatter)
	}

	frontmatterBytes := afterFirst[:endIdx]
	bodyStart := endIdx + len(delimiter)
	// Skip the newline after closing delimiter to handle both \r\n (Windows) and \n (Unix) line endings
	if bodyStart < len(afterFirst) {
		if afterFirst[bodyStart] == '\r' && bodyStart+1 < len(afterFirst) && afterFirst[bodyStart+1] == '\n' {
			bodyStart += 2
		} else if afterFirst[bodyStart] == '\n' {
			bodyStart++
		}
	}
	body := afterFirst[bodyStart:]

	var metadata map[string]any
	if len(bytes.TrimSpace(frontmatterBytes)) > 0 {
		if err := yaml.Unmarshal(frontmatterBytes, &metadata); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidFrontmatter, err)
		}
	} else {
		metadata = make(map[string]any)
	}

	return &Template{
		Metadata: metadata,
		Body:     string(body),
	}, nil
}
