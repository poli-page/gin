package main

import (
	"bytes"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envVars are the POLI_PAGE_* variables setEnv clears before each test.
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

func validAPIKeyEnv(t *testing.T) {
	t.Helper()
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": "pp_sandbox_0123456789abcdef0123456789abcdef"})
}

func TestBuildRequest_ValidProjectMode(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:  "getting-started",
		template: "welcome",
		data:     `{"name":"world"}`,
	}

	req, err := buildRequest(&fs, &bytes.Buffer{})
	require.NoError(t, err)

	assert.Equal(t, polipage.RenderInput{
		Project:  "getting-started",
		Template: "welcome",
		Data:     map[string]any{"name": "world"},
	}, req.Input)
}

func TestBuildRequest_OptionalRenderFlagsPropagate(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:     "p",
		template:    "t",
		data:        `{}`,
		format:      "A4",
		orientation: "landscape",
		locale:      "fr-FR",
	}

	req, err := buildRequest(&fs, &bytes.Buffer{})
	require.NoError(t, err)

	assert.Equal(t, polipage.PageFormatA4, req.Input.Format)
	assert.Equal(t, polipage.OrientationLandscape, req.Input.Orientation)
	assert.Equal(t, "fr-FR", req.Input.Locale)
}

func TestBuildRequest_InvalidJSONInData(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:  "p",
		template: "t",
		data:     "not json",
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data",
		"the error must name the offending flag")
}

func TestBuildRequest_DataRequired(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:  "p",
		template: "t",
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data")
}

func TestBuildRequest_ProjectRequired(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		template: "t",
		data:     `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--project")
}

func TestBuildRequest_TemplateRequired(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project: "p",
		data:    `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--template")
}

func TestBuildRequest_MissingAPIKeySurfacedAsConfigError(t *testing.T) {
	setEnv(t, nil) // no POLI_PAGE_API_KEY
	fs := flagSet{
		project:  "p",
		template: "t",
		data:     `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY",
		"missing env var surfaces through FromEnv with the variable name")
}

// The key selects the environment and the API has no inline render, so
// neither flag exists any more.
func TestRootCmd_HasNoVersionOrInlineTemplateFlag(t *testing.T) {
	flags := newRootCmd().Flags()
	assert.Nil(t, flags.Lookup("template-version"))
	assert.Nil(t, flags.Lookup("inline-template"))
}
