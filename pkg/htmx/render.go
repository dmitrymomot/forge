package htmx

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Renderable is the interface for OOB components.
// Compatible with templ.Component and forge.Component.
type Renderable interface {
	Render(ctx context.Context, w io.Writer) error
}

// Config holds HTMX render configuration.
//
// The fields are intentionally exported. internal/context.go's Render path
// builds a *Config via NewConfig and then needs to iterate OOBComponents to
// render out-of-band components after the main component (see
// internal/context.go Render); the response-header fields are read by
// ApplyHeaders. The supported way to populate a Config is through NewConfig
// plus the With* options, which are append-safe (e.g. WithOOB, WithTrigger);
// direct field mutation is possible but not the intended usage.
type Config struct {
	OOBComponents       []Renderable
	Retarget            string
	Reswap              SwapStrategy
	Reselect            string
	PushURL             string
	ReplaceURL          string
	Triggers            []string
	TriggersAfterSwap   []string
	TriggersAfterSettle []string
	Refresh             bool
}

// RenderOption configures HTMX render behavior.
type RenderOption func(*Config)

// NewConfig creates a Config from options.
func NewConfig(opts ...RenderOption) *Config {
	cfg := &Config{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// ApplyHeaders sets HTMX headers on the response.
// Called by Context.Render() before WriteHeader.
func (c *Config) ApplyHeaders(w http.ResponseWriter) {
	if c == nil {
		return
	}

	h := w.Header()

	if c.Retarget != "" {
		h.Set(HeaderHXRetarget, c.Retarget)
	}
	if c.Reswap != "" {
		h.Set(HeaderHXReswap, string(c.Reswap))
	}
	if c.Reselect != "" {
		h.Set(HeaderHXReselect, c.Reselect)
	}
	if c.PushURL != "" {
		h.Set(HeaderHXPushURL, c.PushURL)
	}
	if c.ReplaceURL != "" {
		h.Set(HeaderHXReplaceURL, c.ReplaceURL)
	}
	if len(c.Triggers) > 0 {
		h.Set(HeaderHXTrigger, encodeTriggers(c.Triggers))
	}
	if len(c.TriggersAfterSwap) > 0 {
		h.Set(HeaderHXTriggerAfterSwap, encodeTriggers(c.TriggersAfterSwap))
	}
	if len(c.TriggersAfterSettle) > 0 {
		h.Set(HeaderHXTriggerAfterSettle, encodeTriggers(c.TriggersAfterSettle))
	}
	if c.Refresh {
		h.Set(HeaderHXRefresh, "true")
	}
}

// encodeTriggers serializes event names into an HX-Trigger header value.
//
// HTMX accepts two header forms: a comma-separated list of event names, or a
// JSON object mapping event names to detail values. The list form is ambiguous
// when a name itself contains a comma, so in that case the JSON object form is
// emitted (names map to null details) to preserve each name verbatim. Otherwise
// the simpler comma-separated form is used.
func encodeTriggers(events []string) string {
	hasComma := false
	for _, e := range events {
		if strings.Contains(e, ",") {
			hasComma = true
			break
		}
	}

	if !hasComma {
		return strings.Join(events, ", ")
	}

	obj := make(map[string]any, len(events))
	for _, e := range events {
		obj[e] = nil
	}
	if data, err := json.Marshal(obj); err == nil {
		return string(data)
	}

	// json.Marshal of map[string]any with nil values cannot fail; fall back to
	// the list form only as a defensive last resort.
	return strings.Join(events, ", ")
}

// WithOOB appends out-of-band components to render after the main component.
// Components must include id and hx-swap-oob attributes.
func WithOOB(components ...Renderable) RenderOption {
	return func(c *Config) {
		c.OOBComponents = append(c.OOBComponents, components...)
	}
}

// WithRetarget sets the HX-Retarget header to change the target element.
func WithRetarget(selector string) RenderOption {
	return func(c *Config) {
		c.Retarget = selector
	}
}

// WithReswap sets the HX-Reswap header to change the swap strategy.
func WithReswap(strategy SwapStrategy) RenderOption {
	return func(c *Config) {
		c.Reswap = strategy
	}
}

// WithReselect sets the HX-Reselect header to select a subset of the response.
func WithReselect(selector string) RenderOption {
	return func(c *Config) {
		c.Reselect = selector
	}
}

// WithPushURL sets the HX-Push-Url header to update browser history.
// Pass "false" to prevent URL update.
func WithPushURL(url string) RenderOption {
	return func(c *Config) {
		c.PushURL = url
	}
}

// WithReplaceURL sets the HX-Replace-Url header to replace current URL.
// Pass "false" to prevent URL replacement.
func WithReplaceURL(url string) RenderOption {
	return func(c *Config) {
		c.ReplaceURL = url
	}
}

// WithTrigger sets the HX-Trigger header to trigger client-side events.
// Multiple events are comma-joined.
func WithTrigger(events ...string) RenderOption {
	return func(c *Config) {
		c.Triggers = append(c.Triggers, events...)
	}
}

// WithTriggerAfterSwap sets the HX-Trigger-After-Swap header.
// Events trigger after the swap completes.
func WithTriggerAfterSwap(events ...string) RenderOption {
	return func(c *Config) {
		c.TriggersAfterSwap = append(c.TriggersAfterSwap, events...)
	}
}

// WithTriggerAfterSettle sets the HX-Trigger-After-Settle header.
// Events trigger after the settle phase.
func WithTriggerAfterSettle(events ...string) RenderOption {
	return func(c *Config) {
		c.TriggersAfterSettle = append(c.TriggersAfterSettle, events...)
	}
}

// WithRefresh sets the HX-Refresh header to force a full page refresh.
func WithRefresh() RenderOption {
	return func(c *Config) {
		c.Refresh = true
	}
}
