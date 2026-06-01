package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	polipagegin "github.com/poli-page/gin"
	polipage "github.com/poli-page/sdk-go"
	"github.com/spf13/cobra"
)

// exitMisuse is the sysexits.h EX_USAGE code: bad flag, missing
// required flag, invalid JSON in --data, etc.
const exitMisuse = 64

// flagSet holds the values bound by the Cobra flag definitions. Kept
// separate from renderRequest so tests can drive buildRequest with a
// constructed flagSet (no need to spin up a Cobra command).
type flagSet struct {
	project         string
	template        string
	templateVersion string
	inlineTemplate  string
	data            string
	format          string
	orientation     string
	locale          string
	outPath         string
	timeout         time.Duration
}

// exitErr is the typed error RunE returns when the run failed with a
// specific non-zero exit code. main() unwraps it for os.Exit; Cobra's
// own flag-parse failures (which bypass RunE) default to exitMisuse.
type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string { return e.err.Error() }
func (e *exitErr) Unwrap() error { return e.err }

// newRootCmd wires the Cobra command. The command is single-purpose —
// no subcommands — so the render runs from the root.
func newRootCmd() *cobra.Command {
	var fs flagSet
	cmd := &cobra.Command{
		Use:           "polipage-render",
		Short:         "Render a Poli Page template to PDF (or HTML, for inline templates).",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoot(cmd, &fs)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&fs.project, "project", "", "Project slug (mutually exclusive with --inline-template)")
	flags.StringVar(&fs.template, "template", "", "Template slug (required with --project)")
	flags.StringVar(&fs.templateVersion, "template-version", "", "Template version, semver (required with --project)")
	flags.StringVar(&fs.inlineTemplate, "inline-template", "", "Inline HTML template body (mutually exclusive with --project)")
	flags.StringVar(&fs.data, "data", "", "JSON object passed to the template (required)")
	flags.StringVar(&fs.format, "format", "", "Page format (A4, Letter, Legal, ...). SDK default applies otherwise.")
	flags.StringVar(&fs.orientation, "orientation", "", "Page orientation: portrait or landscape")
	flags.StringVar(&fs.locale, "locale", "", "BCP 47 locale string")
	flags.StringVarP(&fs.outPath, "out", "o", "", "Output file path (stdout when omitted)")
	flags.DurationVar(&fs.timeout, "timeout", 0, "Per-request timeout (e.g. 30s, 2m). Overrides POLI_PAGE_TIMEOUT.")

	return cmd
}

// runRoot opens the output target (file or stdout), builds the
// renderRequest, calls render(), and translates any failure into an
// *exitErr so main() can read the code.
func runRoot(cmd *cobra.Command, fs *flagSet) error {
	out := cmd.OutOrStdout()
	var fileToClose *os.File
	if fs.outPath != "" {
		f, err := os.Create(fs.outPath)
		if err != nil {
			return &exitErr{code: 4, err: fmt.Errorf("could not create output file %q: %w", fs.outPath, err)}
		}
		fileToClose = f
		out = f
	}
	defer func() {
		if fileToClose != nil {
			_ = fileToClose.Close()
		}
	}()

	req, err := buildRequest(fs, out)
	if err != nil {
		return &exitErr{code: exitMisuse, err: err}
	}

	exitCode, runErr := render(cmd.Context(), req)
	if runErr != nil {
		return &exitErr{code: exitCode, err: runErr}
	}
	return nil
}

// buildRequest validates flags and constructs the renderRequest. It is
// pure — no Cobra, no file I/O (out is supplied by the caller) — so
// tests can drive it with a constructed flagSet.
//
// Returns descriptive errors that name the offending flag; the caller
// (runRoot) wraps these in *exitErr{code: exitMisuse, err}.
func buildRequest(fs *flagSet, out io.Writer) (renderRequest, error) {
	cfg, err := polipagegin.FromEnv()
	if err != nil {
		return renderRequest{}, err
	}
	if fs.timeout > 0 {
		cfg.Timeout = fs.timeout
	}

	if fs.data == "" {
		return renderRequest{}, fmt.Errorf("--data is required (JSON object passed to the template)")
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(fs.data), &data); err != nil {
		return renderRequest{}, fmt.Errorf("--data is not valid JSON: %w", err)
	}

	if fs.project != "" && fs.inlineTemplate != "" {
		return renderRequest{}, fmt.Errorf("--project and --inline-template are mutually exclusive")
	}

	var input polipage.RenderInput
	switch {
	case fs.project != "":
		if fs.template == "" {
			return renderRequest{}, fmt.Errorf("--template is required when --project is set")
		}
		in := polipage.ProjectModeInput{
			Project:     fs.project,
			Template:    fs.template,
			Data:        data,
			Format:      polipage.PageFormat(fs.format),
			Orientation: polipage.Orientation(fs.orientation),
			Locale:      fs.locale,
		}
		if fs.templateVersion != "" {
			in.Version = polipage.Opt(fs.templateVersion)
		}
		input = in
	case fs.inlineTemplate != "":
		input = polipage.InlineModeInput{
			Template:    fs.inlineTemplate,
			Data:        data,
			Format:      polipage.PageFormat(fs.format),
			Orientation: polipage.Orientation(fs.orientation),
			Locale:      fs.locale,
		}
	default:
		return renderRequest{}, fmt.Errorf("one of --project or --inline-template is required")
	}

	return renderRequest{
		Cfg:   cfg,
		Input: input,
		Out:   out,
	}, nil
}
