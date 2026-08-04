// Package config owns machine-local preferences. Configuration may influence
// transport and execution choices, but it never supplies corpus meaning.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const Schema = 1

type Config struct {
	Schema    int       `json:"schema"`
	Lookaside Lookaside `json:"lookaside,omitempty"`
}

type Lookaside struct {
	Cache   string   `json:"cache,omitempty"`
	Mirrors []string `json:"mirrors,omitempty"`
}

func Default() Config { return Config{Schema: Schema} }

func Path() (string, error) {
	if configured := os.Getenv("WALDO_CONFIG"); configured != "" {
		return filepath.Abs(configured)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(dir, "waldo", "config.json"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if config.Schema != Schema {
		return Config{}, fmt.Errorf("%s: unsupported configuration schema %d", path, config.Schema)
	}
	config.Lookaside.Mirrors = normalizeMirrors(config.Lookaside.Mirrors)
	if err := validateMirrors(config.Lookaside.Mirrors); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return config, nil
}

func Save(config Config) error {
	config.Schema = Schema
	config.Lookaside.Mirrors = normalizeMirrors(config.Lookaside.Mirrors)
	if err := validateMirrors(config.Lookaside.Mirrors); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".waldo-config-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func EffectiveCacheRoot(config Config) (string, error) {
	if root := os.Getenv("WALDO_CACHE"); root != "" {
		return filepath.Abs(root)
	}
	if config.Lookaside.Cache != "" {
		return filepath.Abs(config.Lookaside.Cache)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(base, "waldo", "objects"), nil
}

func normalizeMirrors(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func validateMirrors(values []string) error {
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid lookaside mirror %q: %w", value, err)
		}
		switch parsed.Scheme {
		case "", "file", "http", "https", "s3":
		default:
			return fmt.Errorf("lookaside mirror %q uses unsupported scheme %q", value, parsed.Scheme)
		}
	}
	return nil
}
