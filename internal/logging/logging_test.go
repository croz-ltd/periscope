package logging

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{" warn ", slog.LevelWarn},
		{"Error", slog.LevelError},
	} {
		got, err := ParseLevel(tc.in)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	// A typo must be reported, and the message must say what is accepted:
	// silently defaulting leaves someone staring at an empty log.
	err := func() error { _, err := ParseLevel("verbose"); return err }()
	if err == nil {
		t.Fatal("an unknown level must be rejected")
	}
	for _, name := range LevelNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q omits the accepted level %q", err, name)
		}
	}
}

func TestLevelNamesAreOrderedBySeverity(t *testing.T) {
	want := []string{"debug", "info", "warn", "error"}
	got := LevelNames()
	if len(got) != len(want) {
		t.Fatalf("LevelNames() = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("LevelNames() = %v, want %v", got, want)
		}
	}
}

// The level has to actually gate output, which is the whole point of making it
// configurable.
func TestSetupFiltersBelowTheChosenLevel(t *testing.T) {
	var buf bytes.Buffer
	if _, err := setup(&buf, "warn", FormatText); err != nil {
		t.Fatalf("setup: %v", err)
	}

	slog.Debug("debug line")
	slog.Info("info line")
	slog.Warn("warn line")
	slog.Error("error line")

	out := buf.String()
	for _, quiet := range []string{"debug line", "info line"} {
		if strings.Contains(out, quiet) {
			t.Errorf("%q logged at warn, want it filtered out", quiet)
		}
	}
	for _, loud := range []string{"warn line", "error line"} {
		if !strings.Contains(out, loud) {
			t.Errorf("%q is at or above warn but was not logged", loud)
		}
	}
}

func TestSetupJSONFormatCarriesAttributes(t *testing.T) {
	var buf bytes.Buffer
	if _, err := setup(&buf, "info", FormatJSON); err != nil {
		t.Fatalf("setup: %v", err)
	}

	For("scrape").Info("cluster scraped", "cluster", "prod-fra", "components", 42)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json format must emit one JSON object per line: %v (%q)", err, buf.String())
	}
	if entry["msg"] != "cluster scraped" {
		t.Errorf("msg = %v", entry["msg"])
	}
	if entry["component"] != "scrape" {
		t.Errorf("component = %v, want the subsystem tag so logs can be filtered", entry["component"])
	}
	if entry["cluster"] != "prod-fra" {
		t.Errorf("cluster = %v", entry["cluster"])
	}
}

// Anything still writing through the standard log package has to end up in the
// same stream, or it disappears the moment the default logger is replaced.
func TestSetupCapturesStandardLogPackage(t *testing.T) {
	var buf bytes.Buffer
	if _, err := setup(&buf, "info", FormatText); err != nil {
		t.Fatalf("setup: %v", err)
	}

	log.Printf("legacy line %d", 7)

	if !strings.Contains(buf.String(), "legacy line 7") {
		t.Errorf("standard log output was lost: %q", buf.String())
	}
}

func TestSetupRejectsUnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	if _, err := setup(&buf, "info", "xml"); err == nil {
		t.Error("an unknown format must be rejected rather than silently defaulting")
	}
}

// Durations must be readable in JSON too: a log collector showing
// "interval": 600000000000 makes a reader do arithmetic to learn "10m".
func TestSetupRendersDurationsReadably(t *testing.T) {
	var buf bytes.Buffer
	if _, err := setup(&buf, "info", FormatJSON); err != nil {
		t.Fatalf("setup: %v", err)
	}

	slog.Info("starting", "interval", 10*time.Minute)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if entry["interval"] != "10m0s" {
		t.Errorf("interval = %v (%T), want the string 10m0s", entry["interval"], entry["interval"])
	}
}
