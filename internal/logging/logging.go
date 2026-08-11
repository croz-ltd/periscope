// Package logging configures the process-wide structured logger.
//
// Everything logs through log/slog with a "component" attribute naming the
// subsystem, so a noisy fleet can be read by filtering rather than by grepping
// free text. The level is chosen at startup: info is meant to stay readable on
// a fleet of thirty clusters, and debug explains a single scrape in full
// (every extractor, every cluster, every HTTP request).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
)

// Levels accepted by Setup, in order.
var levels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// Formats accepted by Setup. Text reads well in a terminal. A log collector on
// the cluster wants json.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// ParseLevel resolves a level name, case-insensitively.
func ParseLevel(name string) (slog.Level, error) {
	if l, ok := levels[strings.ToLower(strings.TrimSpace(name))]; ok {
		return l, nil
	}
	return 0, fmt.Errorf("unknown log level %q (want one of: %s)", name, strings.Join(LevelNames(), ", "))
}

// LevelNames lists the accepted levels, weakest first, for help text and errors.
func LevelNames() []string {
	names := make([]string, 0, len(levels))
	for n := range levels {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return levels[names[i]] < levels[names[j]] })
	return names
}

// Setup installs the default logger and returns the level it was set to. It
// also redirects anything written through the standard log package, so a
// stray log.Printf in a dependency still lands in the same stream.
func Setup(level, format string) (slog.Level, error) {
	return setup(os.Stderr, level, format)
}

func setup(w io.Writer, level, format string) (slog.Level, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return 0, err
	}

	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: readableDurations}
	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatJSON:
		handler = slog.NewJSONHandler(w, opts)
	case FormatText, "":
		handler = slog.NewTextHandler(w, opts)
	default:
		return 0, fmt.Errorf("unknown log format %q (want %s or %s)", format, FormatText, FormatJSON)
	}

	slog.SetDefault(slog.New(handler))
	return lvl, nil
}

// readableDurations renders durations as "10m0s" rather than as a count of
// nanoseconds. The text handler does this already; JSON does not, and
// "interval": 600000000000 is not something anyone wants to convert in
// their head while reading a log.
func readableDurations(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindDuration {
		return slog.String(a.Key, a.Value.Duration().String())
	}
	return a
}

// For returns a logger tagged with the subsystem it belongs to.
//
// Call it inside a function, never in a package-level variable: it binds to
// whatever the default logger is at that moment, and package variables are
// initialised before Setup chooses a level, which silently pins those loggers
// to the unconfigured default.
func For(component string) *slog.Logger {
	return slog.Default().With("component", component)
}
