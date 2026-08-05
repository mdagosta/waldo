// Package config owns machine-local preferences. Configuration may influence
// transport and execution choices, but it never supplies corpus meaning.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

const Schema = 1

type Config struct {
	Schema    int       `json:"schema"`
	Index     string    `json:"index,omitempty"`
	Lookaside Lookaside `json:"lookaside,omitempty"`
	Ingest    Ingest    `json:"ingest,omitempty"`
	Model     Model     `json:"model,omitempty"`
}

type Ingest struct {
	Staging string `json:"staging,omitempty"`
}

type Model struct {
	Root string `json:"root,omitempty"`
}

type Lookaside struct {
	Cache         string   `json:"cache,omitempty"`
	CacheMaxBytes int64    `json:"cache_max_bytes,omitempty"`
	Scratch       string   `json:"scratch,omitempty"`
	Mirrors       []string `json:"mirrors,omitempty"`
	Publish       *Publish `json:"publish,omitempty"`
}

type Publish struct {
	URL     string `json:"url"`
	Region  string `json:"region,omitempty"`
	Workers int    `json:"workers,omitempty"`
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
	if err := validatePublish(config.Lookaside.Publish); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if config.Lookaside.CacheMaxBytes < 0 {
		return Config{}, fmt.Errorf("%s: lookaside cache maximum must not be negative", path)
	}
	return config, nil
}

func Save(config Config) error {
	config.Schema = Schema
	config.Lookaside.Mirrors = normalizeMirrors(config.Lookaside.Mirrors)
	if err := validateMirrors(config.Lookaside.Mirrors); err != nil {
		return err
	}
	if err := validatePublish(config.Lookaside.Publish); err != nil {
		return err
	}
	if config.Lookaside.CacheMaxBytes < 0 {
		return fmt.Errorf("lookaside cache maximum must not be negative")
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

func validatePublish(publish *Publish) error {
	if publish == nil {
		return nil
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(publish.URL), "/"))
	if err != nil {
		return fmt.Errorf("invalid lookaside publish URL: %w", err)
	}
	switch parsed.Scheme {
	case "s3":
		if parsed.Host == "" {
			return fmt.Errorf("S3 lookaside publish URL requires a bucket")
		}
	case "file":
		if (parsed.Host != "" && parsed.Host != "localhost") || parsed.Path == "" || !filepath.IsAbs(filepath.FromSlash(parsed.Path)) || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("local lookaside publish URL must be an absolute file:/// URL")
		}
		if publish.Region != "" {
			return fmt.Errorf("S3 region cannot be used with a local publisher")
		}
	default:
		return fmt.Errorf("lookaside publish URL must use s3:// or file://")
	}
	publish.URL = parsed.String()
	if publish.Workers == 0 {
		publish.Workers = 4
	}
	if publish.Workers < 1 || publish.Workers > 32 {
		return fmt.Errorf("lookaside publish workers must be in 1..32")
	}
	return nil
}

func EffectiveScratchRoot(config Config) (string, error) {
	if config.Lookaside.Scratch != "" {
		return filepath.Abs(config.Lookaside.Scratch)
	}
	return filepath.Join(temporaryRoot(), "scratch"), nil
}

func EffectiveCacheRoot(config Config) (string, error) {
	if config.Lookaside.Cache != "" {
		return filepath.Abs(config.Lookaside.Cache)
	}
	base, err := durableRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cache"), nil
}

func EffectiveCacheMaxBytes(config Config) int64 {
	if config.Lookaside.CacheMaxBytes > 0 {
		return config.Lookaside.CacheMaxBytes
	}
	return 20 << 30
}

func EffectiveStagingBase(config Config) (string, error) {
	if config.Ingest.Staging != "" {
		return filepath.Abs(config.Ingest.Staging)
	}
	return filepath.Join(temporaryRoot(), "ingest"), nil
}

func EffectiveModelRoot(config Config) (string, error) {
	if config.Model.Root != "" {
		return filepath.Abs(config.Model.Root)
	}
	base, err := durableRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "models"), nil
}

func durableRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".waldo"), nil
}

func temporaryRoot() string {
	name := "waldo"
	if current, err := user.Current(); err == nil && current.Uid != "" {
		uid := strings.Map(func(character rune) rune {
			if character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character == '-' || character == '_' {
				return character
			}
			return '-'
		}, current.Uid)
		if uid != "" {
			name += "-" + uid
		}
	}
	return filepath.Join(os.TempDir(), name)
}

// EffectiveStagingRoot returns a private, plan-specific staging directory.
func EffectiveStagingRoot(config Config, identity string) (string, error) {
	base, err := EffectiveStagingBase(config)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, identity), nil
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
