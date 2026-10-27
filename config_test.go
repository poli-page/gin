package polipagegin_test

import (
	"bytes"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	polipagegin "github.com/poli-page/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keySecret is the 32-hex part of every key fixture: a warning must never
// contain it.
const keySecret = "0123456789abcdef0123456789abcdef"

const sandboxKey = "pp_sandbox_" + keySecret

// envVars is the set of POLI_PAGE_* variables FromEnv reads. setEnv clears
// them all before each test, then applies the test's overrides — keeping
// tests immune to parent-process leakage.
var envVars = []string{
	"POLI_PAGE_API_KEY",
	"POLI_PAGE_BASE_URL",
	"POLI_PAGE_TIMEOUT",
	"POLI_PAGE_MAX_RETRIES",
	"POLI_PAGE_RETRY_DELAY",
}

func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	for _, k := range envVars {
		t.Setenv(k, "")
	}
	for k, v := range overrides {
		t.Setenv(k, v)
	}
}

// captureLog routes slog's default logger into a buffer for the test and
// restores it afterwards. slog.SetDefault also redirects the log package,
// which restoring the previous slog logger does not undo, so log's output
// and flags are restored too.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	prevLogger, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(prevLogger)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

func warnings(buf *bytes.Buffer) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, "level=WARN") {
			out = append(out, line)
		}
	}
	return out
}

func TestFromEnv_AllValidVariables(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_BASE_URL":    "https://api.example.com",
		"POLI_PAGE_TIMEOUT":     "30s",
		"POLI_PAGE_MAX_RETRIES": "5",
		"POLI_PAGE_RETRY_DELAY": "1s",
	})

	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)

	assert.Equal(t, sandboxKey, cfg.APIKey)
	assert.Equal(t, "https://api.example.com", cfg.BaseURL)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, time.Second, cfg.RetryDelay)

	assert.Len(t, cfg.Options(), 5,
		"every set field must produce one option.RequestOption")
}

func TestFromEnv_OnlyAPIKeySet_OptionsContainsOneEntry(t *testing.T) {
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": sandboxKey})

	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Len(t, cfg.Options(), 1,
		"unset optional vars must not emit options; SDK defaults take over")
}

func TestFromEnv_APIKeyEmpty(t *testing.T) {
	setEnv(t, nil)
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig,
		"every config error must wrap ErrInvalidConfig")
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY",
		"the error message must name the offending variable")
}

func TestFromEnv_APIKeyMissing(t *testing.T) {
	setEnv(t, nil)
	require.NoError(t, os.Unsetenv("POLI_PAGE_API_KEY"))
	_, err := polipagegin.FromEnv()
	require.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY")
}

func TestFromEnv_WellFormedKeyLoadsWithoutWarning(t *testing.T) {
	for _, key := range []string{
		sandboxKey,
		"pp_live_" + keySecret,
		"pp_eu-prod_" + keySecret,
		"pp_" + strings.Repeat("a", 20) + "_" + keySecret,
	} {
		t.Run(key, func(t *testing.T) {
			setEnv(t, map[string]string{"POLI_PAGE_API_KEY": key})
			buf := captureLog(t)

			cfg, err := polipagegin.FromEnv()
			require.NoError(t, err)
			assert.Equal(t, key, cfg.APIKey)
			assert.Empty(t, warnings(buf))
		})
	}
}

// malformedKeyWarning is the whole warning text: it names the expected
// format and nothing of the value, since even a grammar-valid pp_{env}_ part
// may be a mis-pasted secret fragment.
const malformedKeyWarning = "polipagegin: POLI_PAGE_API_KEY does not match the expected format " +
	"pp_{environment}_{32 lowercase hex characters}; starting anyway, " +
	"the Poli Page API decides whether the key is valid"

func TestFromEnv_MalformedKeyLoadsWithOneWarningShowingNoKeyText(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		// shown lists key fragments, besides the key itself, that must not
		// appear in the warning.
		shown []string
	}{
		{"grammar-valid prefix, short secret", "pp_sandbox_x", []string{"pp_sandbox_", "sandbox"}},
		{"grammar-valid prefix, 31-hex secret", "pp_sandbox_" + keySecret[:31], []string{"pp_sandbox_", keySecret[:31]}},
		{"grammar-valid prefix, 33-hex secret", "pp_sandbox_" + keySecret + "0", []string{"pp_sandbox_", keySecret}},
		{"grammar-valid prefix, uppercase secret", "pp_sandbox_" + strings.ToUpper(keySecret), []string{"pp_sandbox_", strings.ToUpper(keySecret)}},
		{"grammar-valid hyphenated prefix", "pp_eu-prod_x", []string{"pp_eu-prod_", "eu-prod"}},
		{"grammar-valid 20-character prefix", "pp_" + strings.Repeat("a", 20) + "_x", []string{strings.Repeat("a", 20)}},
		{"hex fragment in the environment slot", "pp_abc123_" + keySecret[:16], []string{"pp_abc123_", "abc123", keySecret[:16]}},
		{"secret in the environment slot", "pp_" + keySecret + "_" + keySecret, []string{keySecret}},
		{"uppercase environment", "pp_Sandbox_" + keySecret, []string{"Sandbox", keySecret}},
		{"environment starting with a digit", "pp_2fa_" + keySecret, []string{"pp_2fa_", keySecret}},
		{"double hyphen in environment", "pp_a--b_" + keySecret, []string{"a--b", keySecret}},
		{"21-character environment", "pp_" + strings.Repeat("a", 21) + "_" + keySecret, []string{strings.Repeat("a", 21), keySecret}},
		{"non-pp_ token", "sk_live_" + keySecret, []string{"sk_live", keySecret}},
		{"trailing character", sandboxKey + "x", []string{"pp_sandbox_", keySecret}},
		{"leading character", "x" + sandboxKey, []string{"pp_sandbox_", keySecret}},
		{"trailing newline", sandboxKey + "\n", []string{"pp_sandbox_", keySecret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, map[string]string{"POLI_PAGE_API_KEY": tc.key})
			buf := captureLog(t)

			cfg, err := polipagegin.FromEnv()
			require.NoError(t, err, "the API, not FromEnv, decides whether a key is valid")
			assert.Equal(t, tc.key, cfg.APIKey)

			got := warnings(buf)
			require.Len(t, got, 1)
			_, record, ok := strings.Cut(got[0], " level=WARN ")
			require.True(t, ok, got[0])
			assert.Equal(t, "msg="+strconv.Quote(malformedKeyWarning), record,
				"the warning carries no attribute, so no key text")
			for _, fragment := range append(tc.shown, strings.TrimSpace(tc.key)) {
				assert.NotContains(t, buf.String(), fragment)
			}
		})
	}
}

func TestFromEnv_EmptyKeyErrorShowsNoKeyText(t *testing.T) {
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": ""})
	buf := captureLog(t)

	_, err := polipagegin.FromEnv()
	require.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Equal(t, "polipagegin: invalid configuration: POLI_PAGE_API_KEY is required", err.Error())
	assert.Empty(t, buf.String())
}

func TestFromEnv_BaseURLUnparseable(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":  sandboxKey,
		"POLI_PAGE_BASE_URL": "://broken",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_BASE_URL")
}

func TestFromEnv_BaseURLNonHTTPScheme(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":  sandboxKey,
		"POLI_PAGE_BASE_URL": "ftp://api.example.com",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_BASE_URL")
	assert.Contains(t, err.Error(), "http",
		"the error must mention the required scheme")
}

func TestFromEnv_TimeoutUnparseable(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY": sandboxKey,
		"POLI_PAGE_TIMEOUT": "not-a-duration",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_TIMEOUT")
	assert.Contains(t, err.Error(), "time.ParseDuration",
		"the error must name the parser whose contract the value violated")
}

func TestFromEnv_TimeoutOutOfRange(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY": sandboxKey,
		"POLI_PAGE_TIMEOUT": "11m",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_TIMEOUT")
	assert.Contains(t, err.Error(), "10m",
		"the error must name the upper bound")
}

func TestFromEnv_TimeoutZeroIsInvalid(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY": sandboxKey,
		"POLI_PAGE_TIMEOUT": "0s",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
}

func TestFromEnv_MaxRetriesNotAnInteger(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_MAX_RETRIES": "not-a-number",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_MAX_RETRIES")
}

func TestFromEnv_MaxRetriesOutOfRange(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_MAX_RETRIES": "99",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_MAX_RETRIES")
	assert.Contains(t, err.Error(), "10",
		"the error must name the upper bound (10)")
}

func TestFromEnv_MaxRetriesNegative(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_MAX_RETRIES": "-1",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
}

func TestFromEnv_MaxRetriesExplicitZeroIsValid(t *testing.T) {
	// The user can explicitly request 0 retries via env. This must produce
	// an option (WithMaxRetries(0)) so the SDK does NOT silently fall back
	// to its default of 2.
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_MAX_RETRIES": "0",
	})
	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.MaxRetries)
	assert.Len(t, cfg.Options(), 2,
		"APIKey + MaxRetries(0) → 2 options; explicit zero must not be treated as unset")
}

func TestFromEnv_RetryDelayUnparseable(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_RETRY_DELAY": "not-a-duration",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_RETRY_DELAY")
	assert.Contains(t, err.Error(), "time.ParseDuration")
}

func TestFromEnv_RetryDelayOutOfRange(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_RETRY_DELAY": "31s",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_RETRY_DELAY")
	assert.Contains(t, err.Error(), "30s")
}

func TestFromEnv_RetryDelayExplicitZeroIsValid(t *testing.T) {
	// Symmetric to MaxRetries=0: an explicit 0 delay (immediate retry,
	// no backoff) must produce an option so the SDK's default is overridden.
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     sandboxKey,
		"POLI_PAGE_RETRY_DELAY": "0s",
	})
	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), cfg.RetryDelay)
	assert.Len(t, cfg.Options(), 2,
		"explicit zero retry delay must emit WithRetryDelay(0)")
}
