package polipagegin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/poli-page/sdk-go/option"
)

// Config is the env-driven configuration for the Poli Page SDK client.
// Populate via FromEnv (which parses POLI_PAGE_* environment variables
// and validates them) and pass Options() to polipage.NewClient.
//
// Fields left at their zero value after FromEnv mean "the env var was
// not set, fall back to the SDK default"; Options omits the corresponding
// option.With* call so polipage.NewClient applies its own defaults
// (DefaultMaxRetries, DefaultRetryDelay, etc.).
type Config struct {
	// APIKey is the Poli Page API key, pp_{environment}_{32 lowercase hex}
	// (pp_sandbox_…, pp_live_…, pp_staging_…). Required and non-empty; any
	// other value is accepted, because the API is the only authority on keys.
	APIKey string
	// BaseURL overrides the API base URL.
	// Must be an http:// or https:// URL when set.
	BaseURL string
	// Timeout is the per-request deadline applied when the caller's
	// context has no deadline of its own. Must be in (0, 10m] when set.
	Timeout time.Duration
	// MaxRetries is the retry budget on top of the initial attempt.
	// Must be in [0, 10] when set. Explicit 0 (no retries) is honored
	// — see Options().
	MaxRetries int
	// RetryDelay is the base exponential-backoff delay. Must be in
	// [0, 30s] when set. Explicit 0 (immediate retry, no backoff) is
	// honored — see Options().
	RetryDelay time.Duration

	// Set-tracking for fields whose zero value is a legitimate user
	// choice (MaxRetries == 0 means "no retries"; RetryDelay == 0
	// means "no backoff"). Without these flags Options() cannot
	// distinguish "unset, take SDK default" from "user said zero".
	setMaxRetries bool
	setRetryDelay bool
}

// ErrInvalidConfig is the sentinel every FromEnv error wraps. Use
// errors.Is(err, polipagegin.ErrInvalidConfig) for a single check when
// the specific failure does not matter to the caller.
var ErrInvalidConfig = errors.New("polipagegin: invalid configuration")

// Environment variable names and validation bounds. Kept together so
// the spec §7.2 contract is auditable in one place.
const (
	envAPIKey     = "POLI_PAGE_API_KEY" //nolint:gosec // G101: env var name, not a credential
	envBaseURL    = "POLI_PAGE_BASE_URL"
	envTimeout    = "POLI_PAGE_TIMEOUT"
	envMaxRetries = "POLI_PAGE_MAX_RETRIES"
	envRetryDelay = "POLI_PAGE_RETRY_DELAY"

	// envNameMaxLen is the longest environment name the API issues keys for.
	envNameMaxLen = 20

	timeoutMax    = 10 * time.Minute
	retryDelayMax = 30 * time.Second
	maxRetriesMin = 0
	maxRetriesMax = 10
)

// apiKeyPattern is the SDK contract's full-key regex
// ^pp_(?=[a-z][a-z0-9-]{0,19}_)[a-z][a-z0-9]*(?:-[a-z0-9]+)*_[0-9a-f]{32}$
// without its lookahead, which RE2 does not support: wellFormedAPIKey
// checks the captured environment name against envNameMaxLen instead.
var apiKeyPattern = regexp.MustCompile(`^pp_([a-z][a-z0-9]*(?:-[a-z0-9]+)*)_[0-9a-f]{32}$`)

// apiKeyEnvPrefix matches the pp_{env}_ part of a key, the most of a key
// that may be logged: the secret follows it. An {env} longer than any
// environment name is not shown, since it may be the secret itself.
var apiKeyEnvPrefix = regexp.MustCompile(fmt.Sprintf(`^pp_[^_]{1,%d}_`, envNameMaxLen))

// FromEnv loads Config from the documented POLI_PAGE_* environment
// variables, validating each one. On failure the returned error wraps
// ErrInvalidConfig and names the offending variable in its message.
//
// FromEnv uses os.LookupEnv (not os.Getenv) so an empty value can be
// distinguished from an unset variable for the optional fields; either
// state is treated as "fall back to the SDK default".
func FromEnv() (Config, error) {
	var cfg Config
	parsers := []func(*Config) error{
		parseAPIKey, parseBaseURL, parseTimeout, parseMaxRetries, parseRetryDelay,
	}
	for _, p := range parsers {
		if err := p(&cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// Options returns the option.RequestOption slice to pass to
// polipage.NewClient. Only fields the user explicitly set (via env)
// contribute — fields left at their zero value defer to the SDK's
// own defaults. Explicit zero for MaxRetries / RetryDelay is honored
// thanks to the internal set-tracking.
func (c Config) Options() []option.RequestOption {
	var opts []option.RequestOption
	if c.APIKey != "" {
		opts = append(opts, option.WithAPIKey(c.APIKey))
	}
	if c.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.BaseURL))
	}
	if c.Timeout > 0 {
		opts = append(opts, option.WithTimeout(c.Timeout))
	}
	if c.setMaxRetries {
		opts = append(opts, option.WithMaxRetries(c.MaxRetries))
	}
	if c.setRetryDelay {
		opts = append(opts, option.WithRetryDelay(c.RetryDelay))
	}
	return opts
}

func parseAPIKey(cfg *Config) error {
	v, ok := os.LookupEnv(envAPIKey)
	if !ok || v == "" {
		return fmt.Errorf("%w: %s is required", ErrInvalidConfig, envAPIKey)
	}
	if !wellFormedAPIKey(v) {
		prefix := "none"
		if p := apiKeyEnvPrefix.FindString(v); p != "" {
			prefix = p + "…"
		}
		slog.Warn(
			"polipagegin: "+envAPIKey+" does not match pp_{environment}_{32 lowercase hex}; "+
				"starting anyway, the Poli Page API decides whether the key is valid",
			"keyPrefix", prefix,
		)
	}
	cfg.APIKey = v
	return nil
}

func wellFormedAPIKey(key string) bool {
	m := apiKeyPattern.FindStringSubmatch(key)
	return len(m) == 2 && len(m[1]) <= envNameMaxLen
}

func parseBaseURL(cfg *Config) error {
	v, ok := os.LookupEnv(envBaseURL)
	if !ok || v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return fmt.Errorf("%w: %s could not be parsed as a URL: %w", ErrInvalidConfig, envBaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"%w: %s must be an http:// or https:// URL (got %q)",
			ErrInvalidConfig, envBaseURL, v,
		)
	}
	cfg.BaseURL = v
	return nil
}

func parseTimeout(cfg *Config) error {
	v, ok := os.LookupEnv(envTimeout)
	if !ok || v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf(
			"%w: %s could not be parsed by time.ParseDuration: %w",
			ErrInvalidConfig, envTimeout, err,
		)
	}
	if d <= 0 || d > timeoutMax {
		return fmt.Errorf(
			"%w: %s must be > 0 and <= %s (got %s)",
			ErrInvalidConfig, envTimeout, timeoutMax, d,
		)
	}
	cfg.Timeout = d
	return nil
}

func parseMaxRetries(cfg *Config) error {
	v, ok := os.LookupEnv(envMaxRetries)
	if !ok || v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf(
			"%w: %s could not be parsed as an integer: %w",
			ErrInvalidConfig, envMaxRetries, err,
		)
	}
	if n < maxRetriesMin || n > maxRetriesMax {
		return fmt.Errorf(
			"%w: %s must be in range [%d, %d] (got %d)",
			ErrInvalidConfig, envMaxRetries, maxRetriesMin, maxRetriesMax, n,
		)
	}
	cfg.MaxRetries = n
	cfg.setMaxRetries = true
	return nil
}

func parseRetryDelay(cfg *Config) error {
	v, ok := os.LookupEnv(envRetryDelay)
	if !ok || v == "" {
		return nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf(
			"%w: %s could not be parsed by time.ParseDuration: %w",
			ErrInvalidConfig, envRetryDelay, err,
		)
	}
	if d < 0 || d > retryDelayMax {
		return fmt.Errorf(
			"%w: %s must be in range [0, %s] (got %s)",
			ErrInvalidConfig, envRetryDelay, retryDelayMax, d,
		)
	}
	cfg.RetryDelay = d
	cfg.setRetryDelay = true
	return nil
}
