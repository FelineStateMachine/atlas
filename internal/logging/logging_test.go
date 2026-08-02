package logging_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FelineStateMachine/atlas/internal/logging"
)

func TestParseLevel(t *testing.T) {
	good := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		" warn ":  slog.LevelWarn,
		"warning": slog.LevelWarn,
		"Error":   slog.LevelError,
	}
	for name, want := range good {
		got, err := logging.ParseLevel(name)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v, want %v", name, got, err, want)
		}
	}
	// A typo is a refusal, not a silent fall back: a run asked for debug and
	// got info has been lied to.
	for _, name := range []string{"", "verbose", "trace", "9"} {
		if _, err := logging.ParseLevel(name); err == nil {
			t.Errorf("ParseLevel(%q) was accepted", name)
		}
	}
}

func TestLevelPrecedence(t *testing.T) {
	cases := []struct {
		name     string
		flag     string
		env      string
		writes   slog.Level
		silences slog.Level
	}{
		{"nothing said", "", "", slog.LevelInfo, slog.LevelDebug},
		{"the environment opens debug", "", "debug", slog.LevelDebug, slog.LevelDebug - 1},
		{"the flag opens debug", "debug", "", slog.LevelDebug, slog.LevelDebug - 1},
		{"the flag beats the environment", "error", "debug", slog.LevelError, slog.LevelWarn},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(logging.LevelEnv, test.env)
			var out bytes.Buffer
			logger, err := logging.New(&out, logging.Options{Level: test.flag})
			if err != nil {
				t.Fatal(err)
			}
			if !logger.Enabled(t.Context(), test.writes) {
				t.Errorf("%v is silenced", test.writes)
			}
			if logger.Enabled(t.Context(), test.silences) {
				t.Errorf("%v is written", test.silences)
			}
		})
	}
}

func TestAnUnreadableLevelIsAUsageError(t *testing.T) {
	if _, err := logging.New(&bytes.Buffer{}, logging.Options{Level: "loud"}); err == nil {
		t.Error("an unreadable --log-level was accepted")
	}
	t.Setenv(logging.LevelEnv, "loud")
	_, err := logging.New(&bytes.Buffer{}, logging.Options{})
	if err == nil || !strings.Contains(err.Error(), logging.LevelEnv) {
		t.Errorf("an unreadable %s gave %v", logging.LevelEnv, err)
	}
}

// Text is the default because a person reads it; JSON is a flag away because
// a machine does. Both carry the same attributes under the same keys.
func TestHandlersCarryTheVocabulary(t *testing.T) {
	attrs := []any{
		logging.Op("install"),
		logging.Volume("westminster-co"),
		logging.World("2026-08-01"),
		logging.Lens("basemap"),
		logging.Stamp("ec3fe8c21cfe"),
		logging.Source("arcgis-hub"),
		logging.Enricher("national"),
		logging.Dur(250 * time.Millisecond),
		logging.Path("/tmp/x.atlas"),
	}

	var text bytes.Buffer
	logger, err := logging.New(&text, logging.Options{})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("bundle installed", attrs...)
	for _, key := range logging.Vocabulary() {
		if !strings.Contains(text.String(), key+"=") {
			t.Errorf("the text stream drops %s: %s", key, text.String())
		}
	}

	var machine bytes.Buffer
	logger, err = logging.New(&machine, logging.Options{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("bundle installed", attrs...)
	var event map[string]any
	if err := json.Unmarshal(machine.Bytes(), &event); err != nil {
		t.Fatalf("the JSON stream is not JSON: %v", err)
	}
	for _, key := range logging.Vocabulary() {
		if _, carried := event[key]; !carried {
			t.Errorf("the JSON stream drops %s: %s", key, machine.String())
		}
	}
	if event["msg"] != "bundle installed" || event["level"] != "INFO" {
		t.Errorf("the event reads as %v", event)
	}
}

func TestBindRegistersTheDocumentedFlags(t *testing.T) {
	var options logging.Options
	fs := flag.NewFlagSet("atlas", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	options.Bind(fs)
	if err := fs.Parse([]string{"--log-json", "--log-level", "debug"}); err != nil {
		t.Fatal(err)
	}
	if !options.JSON || options.Level != "debug" {
		t.Errorf("flags parsed to %+v", options)
	}
}

// The vocabulary and its prose twin must name the same keys, the way the
// conventions registry and REGISTRY.md do.
func TestVocabularyAgreesWithItsDocument(t *testing.T) {
	doc, err := os.ReadFile("../../docs/logging.md")
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `([a-z]+)` \\| (string|duration)")
	documented := make(map[string]bool)
	for _, match := range row.FindAllStringSubmatch(string(doc), -1) {
		documented[match[1]] = true
	}
	for _, key := range logging.Vocabulary() {
		if !documented[key] {
			t.Errorf("%s is in the vocabulary but not documented", key)
			continue
		}
		delete(documented, key)
	}
	for key := range documented {
		t.Errorf("%s is documented but not in the vocabulary", key)
	}

	// The level policy is the other half of the document; each level must
	// appear in its table.
	for _, level := range []string{"debug", "info", "warn", "error"} {
		if !strings.Contains(string(doc), "| `"+level+"` |") {
			t.Errorf("docs/logging.md does not state the %s level's intent", level)
		}
	}
}
