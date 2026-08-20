// Package config loads Mimir's single-file configuration.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the runtime configuration. Every field has an environment
// override so the rune can be configured from Yggdrasil's variable form
// without shipping a config file into the container.
type Config struct {
	// Addr is the listen address.
	Addr string
	// DataDir holds the SQLite database and the game data snapshots.
	DataDir string
	// BaseURL is the public origin, used for PWA manifest and push.
	BaseURL string
	// UserAgent identifies Mimir to Enka.Network. Enka asks integrators for
	// a contactable agent string rather than a generic one.
	UserAgent string
	// SecretKey derives the AES key that encrypts HoYoLAB cookies at rest.
	// Generated on first boot and written to the data dir if unset.
	SecretKey []byte
	// Secure controls the session cookie's Secure flag.
	Secure bool
	// AllowRegistration lets new users sign up. Off by default: this is a
	// personal instance, not a public service.
	AllowRegistration bool
	// Repo is the GitHub repository releases are checked against.
	Repo string
	// LLMBaseURL points at an OpenAI-compatible endpoint (LM Studio, Ollama,
	// vLLM). Empty disables the AI layer entirely; the rest of Mimir works
	// unchanged, because no number depends on the model.
	LLMBaseURL string
	LLMModel   string
	LLMAPIKey  string
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	c := &Config{
		Addr:              env("MIMIR_ADDR", ":8080"),
		DataDir:           env("MIMIR_DATA_DIR", "./data"),
		BaseURL:           env("MIMIR_BASE_URL", "http://localhost:8080"),
		UserAgent:         env("MIMIR_USER_AGENT", "mimir/0.1 (+https://github.com/kristianwind/mimir)"),
		Secure:            envBool("MIMIR_SECURE_COOKIES", false),
		AllowRegistration: envBool("MIMIR_ALLOW_REGISTRATION", false),
		Repo:              env("MIMIR_REPO", "kristianwind/mimir"),
		LLMBaseURL:        env("MIMIR_LLM_BASE_URL", ""),
		LLMModel:          env("MIMIR_LLM_MODEL", ""),
		LLMAPIKey:         env("MIMIR_LLM_API_KEY", ""),
	}

	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("config: create data dir: %w", err)
	}

	key, err := loadOrCreateSecret(filepath.Join(c.DataDir, "secret.key"))
	if err != nil {
		return nil, err
	}
	c.SecretKey = key
	return c, nil
}

// DBPath is the SQLite file path.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "mimir.db") }

// loadOrCreateSecret reads the machine secret, generating one on first boot.
// Losing this file makes stored HoYoLAB cookies undecryptable — which is the
// intended failure mode: a leaked database without the key is inert.
func loadOrCreateSecret(path string) ([]byte, error) {
	if raw, err := os.ReadFile(path); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("config: secret key is corrupt: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("config: secret key must be 32 bytes, got %d", len(key))
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("config: read secret key: %w", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("config: generate secret key: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("config: write secret key: %w", err)
	}
	return key, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
