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
	"time"
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
	// Hosted marks the one instance that is offered as a paid service, and
	// it is off everywhere else.
	//
	// A self-hosted Mimir must never greet its owner with an offer to
	// subscribe to Mimir. They already have it; the whole promise is that
	// running it yourself costs nothing and holds nothing back. So the
	// public pages — what it is, what it costs, the terms — exist only
	// where there is actually something to sell, and every other install
	// keeps going straight to the sign-in form exactly as before.
	Hosted bool

	// Stripe. Empty on every self-hosted install, and then the billing
	// endpoints report themselves unconfigured rather than half-working.
	//
	// The secret key and the webhook secret are credentials and come from
	// the environment only. The publishable key and the price ids are not
	// secret — the first is embedded in every checkout page by design, and
	// the second two name a public catalogue — so they are configured the
	// same way purely for consistency.
	StripeSecretKey      string
	StripeWebhookSecret  string
	StripePublishableKey string
	StripePriceMonthly   string
	StripePriceYearly    string

	// StripeComplaints names any Stripe variable holding the wrong kind of
	// value. Empty when they are all plausible.
	StripeComplaints []string

	// AllowRegistration lets strangers create accounts.
	//
	// It follows Hosted when the variable is absent, which covers running
	// the binary directly. It does NOT cover the Yggdrasil panel: that
	// materialises every declared variable, so an unticked box arrives as an
	// explicit false and wins. Under the panel both boxes have to be ticked,
	// and the rune's hint says so.
	AllowRegistration bool
	// Repo is the GitHub repository releases are checked against.
	Repo string
	// SupplementsPath and EffectsPath are the two hand-maintained data files
	// the miner needs. They ship inside the image, so the defaults point
	// there; a local checkout overrides them.
	SupplementsPath string
	EffectsPath     string
	// LLMBaseURL points at an OpenAI-compatible endpoint (LM Studio, Ollama,
	// vLLM). Empty disables the AI layer entirely; the rest of Mimir works
	// unchanged, because no number depends on the model.
	LLMBaseURL string
	// HeadHTML is markup the operator wants in every page's <head> — an
	// analytics tag, most likely. Environment only: it is script on every
	// page, so it must not be writable over HTTP.
	HeadHTML  string
	LLMModel  string
	LLMAPIKey string
	// LLMThinking lets a reasoning model reason before answering. Off by
	// default: Kvasir wants a JSON object built from a fact sheet, and the
	// chain of thought costs the whole token budget and most of the wait for
	// something nothing reads.
	LLMThinking bool
	// LLMMaxTokens bounds one answer. A reasoning model spends this budget on
	// its chain of thought before it writes anything, so the default is
	// generous — see kvasir.DefaultMaxTokens.
	LLMMaxTokens int
	// LLMTimeout bounds one call to the endpoint. The default sits inside
	// the server's own write timeout on purpose: an answer that arrives
	// after the browser's connection has been closed costs the same tokens
	// as one that arrives in time, and tells nobody anything.
	LLMTimeout time.Duration
}

// Load reads configuration from the environment, applying defaults.
func Load() (*Config, error) {
	c := &Config{
		Addr:                 env("MIMIR_ADDR", ":8080"),
		DataDir:              env("MIMIR_DATA_DIR", "./data"),
		BaseURL:              env("MIMIR_BASE_URL", "http://localhost:8080"),
		UserAgent:            env("MIMIR_USER_AGENT", "mimir/0.1 (+https://github.com/kristianwind/mimir)"),
		Secure:               envBool("MIMIR_SECURE_COOKIES", false),
		Hosted:               envBool("MIMIR_HOSTED", false),
		StripeSecretKey:      env("MIMIR_STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret:  env("MIMIR_STRIPE_WEBHOOK_SECRET", ""),
		StripePublishableKey: env("MIMIR_STRIPE_PUBLISHABLE_KEY", ""),
		StripePriceMonthly:   env("MIMIR_STRIPE_PRICE_MONTHLY", ""),
		StripePriceYearly:    env("MIMIR_STRIPE_PRICE_YEARLY", ""),
		AllowRegistration:    envBool("MIMIR_ALLOW_REGISTRATION", envBool("MIMIR_HOSTED", false)),
		Repo:                 env("MIMIR_REPO", "kristianwind/mimir"),
		SupplementsPath:      env("MIMIR_SUPPLEMENTS", "/etc/mimir/supplements.json"),
		EffectsPath:          env("MIMIR_EFFECTS", "/etc/mimir/effects.json"),
		// Empty by default on every instance. See internal/api/headhtml.go.
		HeadHTML:     env("MIMIR_HEAD_HTML", ""),
		LLMBaseURL:   env("MIMIR_LLM_BASE_URL", ""),
		LLMModel:     env("MIMIR_LLM_MODEL", ""),
		LLMAPIKey:    env("MIMIR_LLM_API_KEY", ""),
		LLMThinking:  envBool("MIMIR_LLM_THINKING", false),
		LLMMaxTokens: envInt("MIMIR_LLM_MAX_TOKENS", 4000),
		LLMTimeout:   time.Duration(envInt("MIMIR_LLM_TIMEOUT", 90)) * time.Second,
	}

	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("config: create data dir %s: %w", c.DataDir, err)
	}
	if err := checkWritable(c.DataDir); err != nil {
		return nil, err
	}

	c.StripeComplaints = c.checkStripe()

	key, err := loadOrCreateSecret(filepath.Join(c.DataDir, "secret.key"))
	if err != nil {
		return nil, err
	}
	c.SecretKey = key
	return c, nil
}

// checkStripe reports fields that hold the wrong kind of value.
//
// Every Stripe credential is a prefixed string, and the five of them sit next
// to each other in a settings form as five boxes of similar-looking gibberish.
// Putting one in the wrong box is a normal mistake, and Stripe's answer to it
// names neither the field nor the product: "Invalid API Key provided:
// whsec_Ma…", at the moment a customer is trying to pay.
//
// This is a complaint and not a refusal. A bad key should not stop the whole
// service booting — the calculations do not need Stripe — but it must not be
// discovered by a customer either, so it is said loudly at startup with the
// variable named.
func (c *Config) checkStripe() []string {
	var out []string
	expect := func(name, value string, prefixes ...string) {
		if value == "" {
			return
		}
		for _, p := range prefixes {
			if strings.HasPrefix(value, p) {
				return
			}
		}
		// The value itself is never quoted back: one of these is a secret,
		// and a log line is exactly where it must not end up. The prefix it
		// *does* start with is enough to say which box it belongs in.
		out = append(out, fmt.Sprintf(
			"%s should start with %s, but starts with %q — is it in the wrong box?",
			name, strings.Join(prefixes, " or "), prefixOf(value)))
	}
	expect("MIMIR_STRIPE_SECRET_KEY", c.StripeSecretKey, "sk_", "rk_")
	expect("MIMIR_STRIPE_WEBHOOK_SECRET", c.StripeWebhookSecret, "whsec_")
	expect("MIMIR_STRIPE_PUBLISHABLE_KEY", c.StripePublishableKey, "pk_")
	expect("MIMIR_STRIPE_PRICE_MONTHLY", c.StripePriceMonthly, "price_")
	expect("MIMIR_STRIPE_PRICE_YEARLY", c.StripePriceYearly, "price_")
	return out
}

// prefixOf returns the part before the first underscore, which is the only
// part of any of these that is safe to print.
func prefixOf(v string) string {
	if i := strings.Index(v, "_"); i > 0 && i < 12 {
		return v[:i+1]
	}
	return "something else"
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

func envInt(key string, def int) int {
	v, err := strconv.Atoi(os.Getenv(key))
	if err != nil || v <= 0 {
		return def
	}
	return v
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

// checkWritable fails early and says exactly what is wrong.
//
// Without it, an unwritable data directory surfaces as whichever file the
// server happens to write first — "write secret key: permission denied" — and
// under a container restart policy that becomes an unexplained boot loop. The
// most common cause is a volume owned by root mounted into an image that runs
// as somebody else, so the message names the uid rather than leaving the
// operator to work it out from a stack of identical log lines.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".mimir-write-probe-*")
	if err == nil {
		name := probe.Name()
		probe.Close()
		_ = os.Remove(name)
		return nil
	}
	return fmt.Errorf(
		"config: the data directory %s is not writable by uid %d (gid %d). "+
			"Usually the volume is owned by root while the image runs as another user: "+
			"chown -R %d:%d %s on the host, or run the container as root "+
			"(user: \"0:0\" in the rune). Underlying error: %w",
		dir, os.Getuid(), os.Getgid(), os.Getuid(), os.Getgid(), dir, err)
}
