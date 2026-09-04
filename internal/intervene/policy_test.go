package intervene_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n1tishc/mulch/internal/intervene"
)

func TestLoadPolicyOverridesDefaultsAndRejectsInvalidThresholdOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(`{"warn_below":90,"cooldown_turns":4,"max_prune_share":0.2}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := intervene.LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.WarnBelow != 90 || got.CooldownTurns != 4 || got.PruneBelow != 65 || got.MaxPruneShare != .2 {
		t.Fatalf("policy = %#v", got)
	}
	if err := os.WriteFile(path, []byte(`{"warn_below":40,"prune_below":70}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := intervene.LoadPolicy(path); err == nil {
		t.Fatal("invalid threshold order accepted")
	}
}
