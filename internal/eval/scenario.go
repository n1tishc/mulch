package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Scenario struct {
	ProtectedFiles  []string    `yaml:"protected_files" json:"protected_files"`
	Grader          string      `yaml:"grader" json:"grader"`
	Name            string      `yaml:"name" json:"name"`
	Task            string      `yaml:"task" json:"task"`
	WorkdirFixture  string      `yaml:"workdir_fixture" json:"workdir_fixture"`
	MaxTurns        int         `yaml:"max_turns" json:"max_turns"`
	SuccessCommands []string    `yaml:"success_check" json:"success_check"`
	Injections      []Injection `yaml:"injections" json:"injections"`
}

type Injection struct {
	Kind           string `yaml:"kind" json:"kind"`
	Text           string `yaml:"text" json:"text"`
	Turn           int    `yaml:"turn" json:"turn"`
	SourceTSOffset string `yaml:"source_ts_offset" json:"source_ts_offset,omitempty"`
}

func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	var scenario Scenario
	if err := yaml.Unmarshal(data, &scenario); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario: %w", err)
	}
	if scenario.Task == "" || scenario.WorkdirFixture == "" || scenario.MaxTurns < 1 || (len(scenario.SuccessCommands) == 0 && scenario.Grader == "") {
		return Scenario{}, fmt.Errorf("scenario requires task, workdir_fixture, positive max_turns, and success_check or grader")
	}
	if !filepath.IsAbs(scenario.WorkdirFixture) {
		scenario.WorkdirFixture = filepath.Join(filepath.Dir(path), scenario.WorkdirFixture)
	}
	if scenario.Grader != "" {
		if !filepath.IsAbs(scenario.Grader) {
			scenario.Grader = filepath.Join(filepath.Dir(path), scenario.Grader)
		}
		var err error
		scenario.Grader, err = filepath.Abs(scenario.Grader)
		if err != nil {
			return Scenario{}, err
		}
		if len(scenario.SuccessCommands) > 0 {
			return Scenario{}, fmt.Errorf("choose grader or success_check, not both")
		}
		info, statErr := os.Stat(scenario.Grader)
		if statErr != nil {
			return Scenario{}, fmt.Errorf("grader: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			return Scenario{}, fmt.Errorf("grader must be a regular file")
		}
		fixture, absErr := filepath.Abs(scenario.WorkdirFixture)
		if absErr != nil {
			return Scenario{}, absErr
		}
		relative, relErr := filepath.Rel(fixture, scenario.Grader)
		if relErr == nil && filepath.IsLocal(relative) {
			return Scenario{}, fmt.Errorf("hidden grader must be outside workdir_fixture")
		}
	}
	for _, name := range scenario.ProtectedFiles {
		if !filepath.IsLocal(name) {
			return Scenario{}, fmt.Errorf("protected file must be relative: %s", name)
		}
	}
	for i := range scenario.Injections {
		if scenario.Injections[i].Turn < 1 {
			scenario.Injections[i].Turn = 1
		}
		switch scenario.Injections[i].Kind {
		case "stale_tool_result":
			if _, err := time.ParseDuration(scenario.Injections[i].SourceTSOffset); err != nil {
				return Scenario{}, fmt.Errorf("injection %d source_ts_offset: %w", i, err)
			}
		case "contradiction", "distractor":
		default:
			return Scenario{}, fmt.Errorf("injection %d has unsupported kind %q", i, scenario.Injections[i].Kind)
		}
	}
	return scenario, nil
}
