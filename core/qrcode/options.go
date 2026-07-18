package qrcode

import (
	"image"
	"image/color"
)

// Option configures a QR render/encode call.
type Option func(*config)

// WithLevel sets the error-correction level (default LevelM).
func WithLevel(l Level) Option { return func(c *config) { c.level = l } }

// WithScale sets the number of pixels per module for raster output (default 8).
// Integer scaling keeps modules crisp. Affects PNG/DataURI only; SVG scales
// freely via its module-unit viewBox and ignores this.
func WithScale(pxPerModule int) Option { return func(c *config) { c.scale = pxPerModule } }

// WithSize requests an approximate output width in pixels; the renderer picks
// the largest integer scale whose full image (modules + border) fits. Wins
// over WithScale when both are set. Affects PNG/DataURI only; SVG scales freely
// via its module-unit viewBox and ignores this.
func WithSize(targetPx int) Option { return func(c *config) { c.targetSize = targetPx } }

// WithBorder sets the quiet-zone width in modules (default 4).
func WithBorder(modules int) Option { return func(c *config) { c.border = modules } }

// WithForeground sets the dark-module color (default black).
func WithForeground(col color.Color) Option { return func(c *config) { c.fg = col } }

// WithBackground sets the background color (default white).
func WithBackground(col color.Color) Option { return func(c *config) { c.bg = col } }

// WithLogo overlays a centered, caller-decoded image on PNG/SVG output. A logo
// raises the effective error-correction level to at least LevelQ.
func WithLogo(img image.Image) Option { return func(c *config) { c.logo = img } }

// WithLogoSize sets the logo width as a fraction of the code width (default
// 0.2). Must be in (0, 0.3]; out-of-range values make the encode/render call
// return ErrInvalidLogoSize.
func WithLogoSize(frac float64) Option { return func(c *config) { c.logoSize = frac } }

// WithModuleShape styles the data modules (default ShapeSquare).
func WithModuleShape(s Shape) Option { return func(c *config) { c.moduleShape = s } }

// WithEyeShape styles the finder patterns (default EyeSquare).
func WithEyeShape(e EyeShape) Option { return func(c *config) { c.eyeShape = e } }
