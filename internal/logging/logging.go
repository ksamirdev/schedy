// Package logging configures Schedy's structured logger.
//
// Every line carries the task id it is about, so an operator tracing one
// delivery can filter to it instead of eyeballing interleaved output from
// however many deliveries are in flight - which, with a concurrency cap of 50,
// is what the old unstructured format actually looked like under load.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup installs the default slog logger from the environment.
//
// SCHEDY_LOG_FORMAT is "text" (default, human-readable) or "json" for log
// shippers. SCHEDY_LOG_LEVEL is debug, info (default), warn, or error.
//
// An unrecognised value falls back to the default rather than refusing to boot:
// a typo in a log setting must never be the reason a scheduler won't start.
func Setup() {
	opts := &slog.HandlerOptions{Level: parseLevel(os.Getenv("SCHEDY_LOG_LEVEL"))}

	var handler slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if strings.EqualFold(os.Getenv("SCHEDY_LOG_FORMAT"), "json") {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
