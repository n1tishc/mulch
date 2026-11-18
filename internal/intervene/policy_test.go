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

func TestLoadPolicyValidatesSevereThresholdsAndAllowsDisablingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, []byte(`{"severe_coherence_below":1.5}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := intervene.LoadPolicy(path); err == nil {
		t.Fatal("severe threshold above 1 accepted")
	}
	if err := os.WriteFile(path, []byte(`{"severe_coherence_below":0,"severe_staleness_below":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := intervene.LoadPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SevereCoherenceBelow != 0 || got.SevereStalenessBelow != 0 || got.SevereRelevanceBelow != intervene.DefaultPolicy().SevereRelevanceBelow {
		t.Fatalf("explicit zero must disable only the named thresholds: %#v", got)
	}
}
