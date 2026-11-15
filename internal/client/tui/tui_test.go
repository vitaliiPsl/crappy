package tui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/workdir"
)

func TestInitialSessionUsesLaunchDirectory(t *testing.T) {
	t.Parallel()

	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")

	var sessions []state.Record
	for _, value := range []struct{ id, directory string }{
		{"other-project", second},
		{"recent", first},
		{"older", first},
	} {
		encoded, err := json.Marshal(value.directory)
		if err != nil {
			t.Fatal(err)
		}

		sessions = append(sessions, state.Record{
			ID:     value.id,
			Values: map[string]json.RawMessage{workdir.StateKey: encoded},
		})
	}

	sessions = append([]state.Record{{ID: "no-workdir"}}, sessions...)

	for _, test := range []struct{ directory, want string }{
		{first, "recent"},
		{second, "other-project"},
		{t.TempDir(), ""},
	} {
		got, err := initialSession(sessions, test.directory)
		if err != nil || got != test.want {
			t.Fatalf("initial session for %q = %q, %v; want %q", test.directory, got, err, test.want)
		}
	}
}
