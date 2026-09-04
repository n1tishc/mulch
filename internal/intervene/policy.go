package intervene

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func LoadPolicy(path string) (Policy, error) {
	file, err := os.Open(path)
	if err != nil {
		return Policy{}, fmt.Errorf("open policy: %w", err)
	}
	defer file.Close()
	policy := DefaultPolicy()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return Policy{}, fmt.Errorf("decode policy: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Policy{}, errors.New("decode policy: multiple JSON values")
	}
	if err := policy.Validate(); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func (p Policy) Validate() error {
	if !(p.WarnBelow > p.PruneBelow && p.PruneBelow > p.CompactBelow && p.CompactBelow > p.ReanchorBelow && p.ReanchorBelow > p.EscalateBelow && p.EscalateBelow >= 0 && p.WarnBelow <= 100) {
		return errors.New("invalid policy: thresholds must satisfy 100 >= warn > prune > compact > reanchor > escalate >= 0")
	}
	if p.Hysteresis < 0 || p.Hysteresis > 100 {
		return errors.New("invalid policy: hysteresis must be between 0 and 100")
	}
	if p.CooldownTurns < 0 || p.ConfirmTurns < 1 {
		return errors.New("invalid policy: cooldown_turns must be non-negative and confirm_turns positive")
	}
	if p.MaxPruneShare <= 0 || p.MaxPruneShare > 1 {
		return errors.New("invalid policy: max_prune_share must be in (0,1]")
	}
	return nil
}
