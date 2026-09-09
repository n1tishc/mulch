package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"
)

var configKeys = map[string]string{
	"api-key": "MULCH_PROVIDER_API_KEY", "base-url": "MULCH_PROVIDER_BASE_URL",
	"model": "MULCH_MODEL", "judge-model": "MULCH_JUDGE_MODEL",
	"embedding-api-key": "MULCH_EMBEDDING_API_KEY", "embedding-base-url": "MULCH_EMBEDDING_BASE_URL",
	"embedding-model": "MULCH_EMBEDDING_MODEL",
}

func configPath(getenv func(string) string) string {
	if path := getenv("MULCH_CONFIG"); path != "" {
		return path
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home := getenv("HOME"); home != "" {
			base = filepath.Join(home, ".config")
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "mulch", "config.json")
}

func readConfig(path string) (map[string]string, error) {
	values := map[string]string{}
	if path == "" {
		return values, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("invalid config in %s: expected a JSON object of strings", path)
	}
	if values == nil {
		return nil, errors.New("config must be a JSON object")
	}
	for key, value := range values {
		if err = validateConfig(key, value); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func validateConfig(key, value string) error {
	if _, ok := configKeys[key]; !ok {
		return fmt.Errorf("unknown configuration key %q", key)
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s cannot be empty; use config unset", key)
	}
	if strings.HasSuffix(key, "base-url") {
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s must be an HTTP(S) URL without credentials, query, or fragment", key)
		}
	}
	return nil
}

func configuredEnvironment(getenv func(string) string) (func(string) string, error) {
	values, err := readConfig(configPath(getenv))
	if err != nil {
		return nil, err
	}
	defaults := map[string]string{}
	for key, value := range values {
		defaults[configKeys[key]] = value
	}
	return func(key string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return defaults[key]
	}, nil
}

func configure(args []string, opts Options) error {
	path := configPath(opts.Getenv)
	if len(args) == 0 || args[0] == "--help" {
		_, err := fmt.Fprintln(opts.Stdout, "Provider configuration\n\n  mulch config set base-url https://opencode.ai/zen/go/v1\n  mulch config set model glm-5.3-flash\n  mulch config set api-key              # hidden prompt; not shell history\n  mulch config set api-key --stdin      # read from a pipe\n  mulch config show                    # saved values; secrets redacted\n  mulch config path\n  mulch config unset KEY\n\nEnvironment and .env override saved values. Restart Mulch after changes.")
		return err
	}
	if path == "" {
		return errors.New("set HOME, XDG_CONFIG_HOME, or MULCH_CONFIG to locate the config file")
	}
	if args[0] == "path" && len(args) == 1 {
		_, err := fmt.Fprintln(opts.Stdout, path)
		return err
	}
	values, err := readConfig(path)
	if err != nil {
		return err
	}
	if args[0] == "show" && len(args) == 1 {
		for key := range values {
			if strings.HasSuffix(key, "api-key") {
				values[key] = "[redacted]"
			}
		}
		return json.NewEncoder(opts.Stdout).Encode(values)
	}
	if len(args) < 2 {
		return errors.New("use mulch config --help")
	}
	key := args[1]
	if _, ok := configKeys[key]; !ok {
		return fmt.Errorf("unknown configuration key %q", key)
	}
	switch args[0] {
	case "unset":
		if len(args) != 2 {
			return errors.New("usage: mulch config unset KEY")
		}
		delete(values, key)
	case "set":
		value := ""
		secret := strings.HasSuffix(key, "api-key")
		switch {
		case len(args) == 3 && args[2] == "--stdin":
			data, readErr := io.ReadAll(io.LimitReader(opts.Stdin, 65537))
			if readErr != nil {
				return readErr
			}
			if len(data) > 65536 {
				return errors.New("configuration value too long")
			}
			value = strings.TrimSpace(string(data))
		case len(args) == 2 && secret:
			input, ok := opts.Stdin.(*os.File)
			if !ok || !term.IsTerminal(input.Fd()) {
				return errors.New("use --stdin to read the key from a pipe")
			}
			_, _ = fmt.Fprint(opts.Stderr, "API key (hidden): ")
			data, readErr := term.ReadPassword(input.Fd())
			_, _ = fmt.Fprintln(opts.Stderr)
			if readErr != nil {
				return readErr
			}
			value = strings.TrimSpace(string(data))
		case len(args) == 3 && !secret:
			value = strings.TrimSpace(args[2])
		default:
			return errors.New("use config set KEY VALUE; for API keys omit VALUE or use --stdin")
		}
		if err = validateConfig(key, value); err != nil {
			return err
		}
		values[key] = value
	default:
		return errors.New("use mulch config --help")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	_, err = fmt.Fprintf(opts.Stdout, "Saved %s in %s. Restart Mulch to apply.\n", key, path)
	return err
}
