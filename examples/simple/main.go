// Package main demonstrates core Forge features in a single-file example.
// No external dependencies required (no database, no templates).
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dmitrymomot/forge"
	"github.com/dmitrymomot/forge/pkg/logger"
)

func main() {
	slog := logger.New().With("app", "simple")

	app := forge.New(
		forge.WithCustomLogger(slog),
		forge.WithMiddleware(loggingMiddleware),
		forge.WithHandlers(
			&greetingHandler{},
			&echoHandler{},
		),
		forge.WithHealthChecks(),
		forge.WithErrorHandler(handleError),
		forge.WithNotFoundHandler(handleNotFound),
	)

	slog.Info("starting server", "addr", ":8080")

	if err := app.Run(
		":8080",
		forge.Logger(slog),
		forge.ShutdownTimeout(10*time.Second),
	); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

// --- Middleware ---

// loggingMiddleware logs each incoming request.
func loggingMiddleware(next forge.HandlerFunc) forge.HandlerFunc {
	return func(c forge.Context) error {
		start := time.Now()
		c.LogInfo("request started",
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
		)

		err := next(c)

		c.LogInfo("request completed",
			"method", c.Request().Method,
			"path", c.Request().URL.Path,
			"duration", time.Since(start).String(),
		)
		return err
	}
}

// --- Greeting Handler ---

// greetingHandler demonstrates basic routing patterns.
type greetingHandler struct{}

func (h *greetingHandler) Routes(r forge.Router) {
	r.GET("/", h.home)
	r.GET("/hello/{name}", h.helloName)
	r.GET("/greet", h.greetQuery)
}

// home returns a welcome message.
func (h *greetingHandler) home(c forge.Context) error {
	return c.String(http.StatusOK, "Welcome to Forge!")
}

// helloName greets using a URL path parameter.
func (h *greetingHandler) helloName(c forge.Context) error {
	name := c.Param("name")
	return c.String(http.StatusOK, fmt.Sprintf("Hello, %s!", name))
}

// greetQuery greets using a query parameter.
func (h *greetingHandler) greetQuery(c forge.Context) error {
	name := c.QueryDefault("name", "Guest")
	return c.String(http.StatusOK, fmt.Sprintf("Hello, %s!", name))
}

// --- Echo Handler ---

// echoHandler demonstrates JSON binding and validation.
type echoHandler struct{}

func (h *echoHandler) Routes(r forge.Router) {
	r.POST("/echo", h.echo)
}

// echoRequest represents the JSON request body.
type echoRequest struct {
	Message string `json:"message" validate:"required;max:100" san:"trim,xss"`
}

// echoResponse represents the JSON response body.
type echoResponse struct {
	Original  string `json:"original"`
	Uppercase string `json:"uppercase"`
	Length    int    `json:"length"`
}

// echo echoes back the received message with transformations.
func (h *echoHandler) echo(c forge.Context) error {
	var req echoRequest
	if validationErrs, err := c.BindJSON(&req); err != nil {
		return fmt.Errorf("bind error: %w", err)
	} else if len(validationErrs) > 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": validationErrs,
		})
	}

	resp := echoResponse{
		Original:  req.Message,
		Uppercase: uppercase(req.Message),
		Length:    len(req.Message),
	}
	return c.JSON(http.StatusOK, resp)
}

// uppercase converts a string to uppercase without importing strings.
func uppercase(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// --- Error Handlers ---

// handleError handles errors returned from handlers.
func handleError(c forge.Context, err error) error {
	c.LogError("handler error", "error", err)
	return c.JSON(http.StatusInternalServerError, map[string]string{
		"error": err.Error(),
	})
}

// handleNotFound handles requests to unknown routes.
func handleNotFound(c forge.Context) error {
	return c.JSON(http.StatusNotFound, map[string]string{
		"error": "not found",
	})
}
