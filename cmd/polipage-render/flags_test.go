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
	setEnv(t, map[string]string{"POLI_PAGE_API_KEY": "pp_test_unit"})
}

func TestBuildRequest_ValidProjectMode(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:         "getting-started",
		template:        "welcome",
		templateVersion: "1.0.0",
		data:            `{"name":"world"}`,
	}

	req, err := buildRequest(&fs, &bytes.Buffer{})
	require.NoError(t, err)

	in, ok := req.Input.(polipage.ProjectModeInput)
	require.True(t, ok, "project mode flags must produce a ProjectModeInput, got %T", req.Input)
	assert.Equal(t, "getting-started", in.Project)
	assert.Equal(t, "welcome", in.Template)
	require.NotNil(t, in.Version, "Version must be a non-nil *string when --template-version is set")
	assert.Equal(t, "1.0.0", *in.Version)
	assert.Equal(t, map[string]any{"name": "world"}, in.Data)
}

func TestBuildRequest_ValidInlineMode(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		inlineTemplate: "<h1>Hello {{ name }}</h1>",
		data:           `{"name":"world"}`,
	}

	req, err := buildRequest(&fs, &bytes.Buffer{})
	require.NoError(t, err)

	in, ok := req.Input.(polipage.InlineModeInput)
	require.True(t, ok, "inline-template flag must produce an InlineModeInput, got %T", req.Input)
	assert.Equal(t, "<h1>Hello {{ name }}</h1>", in.Template)
	assert.Equal(t, map[string]any{"name": "world"}, in.Data)
}

func TestBuildRequest_OptionalRenderFlagsPropagate(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:         "p",
		template:        "t",
		templateVersion: "1.0.0",
		data:            `{}`,
		format:          "A4",
		orientation:     "landscape",
		locale:          "fr-FR",
	}

	req, err := buildRequest(&fs, &bytes.Buffer{})
	require.NoError(t, err)

	in, ok := req.Input.(polipage.ProjectModeInput)
	require.True(t, ok)
	assert.Equal(t, polipage.PageFormatA4, in.Format)
	assert.Equal(t, polipage.OrientationLandscape, in.Orientation)
	assert.Equal(t, "fr-FR", in.Locale)
}

func TestBuildRequest_InvalidJSONInData(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:         "p",
		template:        "t",
		templateVersion: "1.0.0",
		data:            "not json",
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data",
		"the error must name the offending flag")
}

func TestBuildRequest_DataRequired(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:         "p",
		template:        "t",
		templateVersion: "1.0.0",
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--data")
}

func TestBuildRequest_NeitherProjectNorInlineTemplate(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		data: `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--project")
	assert.Contains(t, err.Error(), "--inline-template",
		"the error must hint at both alternatives")
}

func TestBuildRequest_ProjectAndInlineMutuallyExclusive(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project:         "p",
		template:        "t",
		templateVersion: "1.0.0",
		inlineTemplate:  "<h1></h1>",
		data:            `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestBuildRequest_ProjectRequiresTemplate(t *testing.T) {
	validAPIKeyEnv(t)
	fs := flagSet{
		project: "p",
		// template missing
		templateVersion: "1.0.0",
		data:            `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--template")
}

func TestBuildRequest_MissingAPIKeySurfacedAsConfigError(t *testing.T) {
	setEnv(t, nil) // no POLI_PAGE_API_KEY
	fs := flagSet{
		project:         "p",
		template:        "t",
		templateVersion: "1.0.0",
		data:            `{}`,
	}

	_, err := buildRequest(&fs, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "POLI_PAGE_API_KEY",
		"missing env var surfaces through FromEnv with the variable name")
}
