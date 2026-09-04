package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadScenarioDefinesFixtureChecksAndPoisoning(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "scenario.yaml")
	data := []byte("name: stale context\ntask: fix the answer\nworkdir_fixture: fixture\nmax_turns: 3\nsuccess_check:\n  - test -f answer.txt\ninjections:\n  - kind: stale_tool_result\n    turn: 2\n    text: old answer\n    source_ts_offset: -24h\n  - kind: contradiction\n    text: do the opposite\n  - kind: distractor\n    text: irrelevant detail\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadScenario(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkdirFixture != filepath.Join(root, "fixture") || len(got.Injections) != 3 || got.Injections[1].Turn != 1 {
		t.Fatalf("scenario = %+v", got)
	}
}
