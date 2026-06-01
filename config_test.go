package polipagegin_test

import (
	"testing"
	"time"

	polipagegin "github.com/poli-page/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestFromEnv_AllValidVariables(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     "pp_test_abc123",
		"POLI_PAGE_BASE_URL":    "https://api-develop.poli.page",
		"POLI_PAGE_TIMEOUT":     "30s",
		"POLI_PAGE_MAX_RETRIES": "5",
		"POLI_PAGE_RETRY_DELAY": "1s",
	})

	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)

	assert.Equal(t, "pp_test_abc123", cfg.APIKey)
	assert.Equal(t, "https://api-develop.poli.page", cfg.BaseURL)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, 5, cfg.MaxRetries)
	assert.Equal(t, time.Second, cfg.RetryDelay)

	assert.Len(t, cfg.Options(), 5,
		"every set field must produce one option.RequestOption")
}

func TestFromEnv_OnlyAPIKeySet_OptionsContainsOneEntry(t *testing.T) {
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": "pp_live_abc"})

	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Len(t, cfg.Options(), 1,
		"unset optional vars must not emit options; SDK defaults take over")
}

func TestFromEnv_LiveAPIKeyAccepted(t *testing.T) {
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": "pp_live_real"})
	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, "pp_live_real", cfg.APIKey)
}

func TestFromEnv_APIKeyMissing(t *testing.T) {
	setEnv(t, nil)
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig,
		"every config error must wrap ErrInvalidConfig")
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY",
		"the error message must name the offending variable")
}

func TestFromEnv_APIKeyWrongPrefix(t *testing.T) {
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": "wrong-format-key"})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY")
	assert.Contains(t, err.Error(), "pp_test_",
		"the error must hint at the expected prefix shape")
	assert.Contains(t, err.Error(), "pp_live_")
}

func TestFromEnv_BaseURLUnparseable(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":  "pp_test_x",
		"POLI_PAGE_BASE_URL": "://broken",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_BASE_URL")
}

func TestFromEnv_BaseURLNonHTTPScheme(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":  "pp_test_x",
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
		"POLI_PAGE_API_KEY": "pp_test_x",
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
		"POLI_PAGE_API_KEY": "pp_test_x",
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
		"POLI_PAGE_API_KEY": "pp_test_x",
		"POLI_PAGE_TIMEOUT": "0s",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
}

func TestFromEnv_MaxRetriesNotAnInteger(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     "pp_test_x",
		"POLI_PAGE_MAX_RETRIES": "not-a-number",
	})
	_, err := polipagegin.FromEnv()
	require.Error(t, err)
	assert.ErrorIs(t, err, polipagegin.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "POLI_PAGE_MAX_RETRIES")
}

func TestFromEnv_MaxRetriesOutOfRange(t *testing.T) {
	setEnv(t, map[string]string{
		"POLI_PAGE_API_KEY":     "pp_test_x",
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
		"POLI_PAGE_API_KEY":     "pp_test_x",
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
		"POLI_PAGE_API_KEY":     "pp_test_x",
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
		"POLI_PAGE_API_KEY":     "pp_test_x",
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
		"POLI_PAGE_API_KEY":     "pp_test_x",
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
		"POLI_PAGE_API_KEY":     "pp_test_x",
		"POLI_PAGE_RETRY_DELAY": "0s",
	})
	cfg, err := polipagegin.FromEnv()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), cfg.RetryDelay)
	assert.Len(t, cfg.Options(), 2,
		"explicit zero retry delay must emit WithRetryDelay(0)")
}
