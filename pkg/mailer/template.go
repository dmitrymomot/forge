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
	// Split content by frontmatter delimiters (---)
	delimiter := []byte("---")

	// Check if content starts with delimiter
	if !bytes.HasPrefix(content, delimiter) {
		// No frontmatter, return empty metadata and full content as body
		return &Template{
			Metadata: make(map[string]any),
			Body:     string(content),
		}, nil
	}

	// Find the end of frontmatter (second occurrence of ---)
	afterFirst := bytes.TrimPrefix(content, delimiter)
	afterFirst = bytes.TrimLeft(afterFirst, "\n\r")

	// Ensure we have content to parse
	if len(afterFirst) == 0 {
		return nil, fmt.Errorf("%w: no content after opening delimiter", ErrInvalidFrontmatter)
	}

	endIdx := bytes.Index(afterFirst, delimiter)
	if endIdx == -1 {
		return nil, fmt.Errorf("%w: closing delimiter not found", ErrInvalidFrontmatter)
	}

	// Extract frontmatter and body
	frontmatterBytes := afterFirst[:endIdx]
	bodyStart := endIdx + len(delimiter)
	// Skip one newline after closing delimiter (handles both \r\n and \n)
	if bodyStart < len(afterFirst) {
		if afterFirst[bodyStart] == '\r' && bodyStart+1 < len(afterFirst) && afterFirst[bodyStart+1] == '\n' {
			bodyStart += 2 // Skip \r\n
		} else if afterFirst[bodyStart] == '\n' {
			bodyStart++ // Skip \n
		}
	}
	body := afterFirst[bodyStart:]

	// Parse YAML frontmatter
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
