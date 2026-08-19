// Package output implements the CLI output protocol of cli-contract.md §21/§22:
// with --output json, stdout carries exactly one JSON document; progress and
// human messages go to stderr only, always token-redacted.
package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// Format selects the rendering mode.
type Format string

const (
	FormatJSON  Format = "json"
	FormatTable Format = "table"
)

// ParseFormat validates a user-supplied output format value.
func ParseFormat(s string) (Format, error) {
	switch s {
	case string(FormatJSON):
		return FormatJSON, nil
	case string(FormatTable):
		return FormatTable, nil
	default:
		return "", clierr.Usage("无效的 --output 值 %q，只支持 table 或 json", s)
	}
}

// SchemaVersion is the fixed envelope schema version (cli-contract.md §3).
const SchemaVersion = "1"

// Envelope is the single JSON document written to stdout.
type Envelope struct {
	SchemaVersion string         `json:"schemaVersion"`
	Command       string         `json:"command"`
	OK            bool           `json:"ok"`
	Data          any            `json:"data"`
	Error         *ErrorBody     `json:"error,omitempty"`
	Meta          map[string]any `json:"meta"`
}

// ErrorBody mirrors cli-contract.md §21.
type ErrorBody struct {
	Kind       string          `json:"kind"`
	HTTPStatus int             `json:"httpStatus,omitempty"`
	Code       int             `json:"code,omitempty"`
	Message    string          `json:"message"`
	Details    json.RawMessage `json:"details,omitempty"`
}

// Printer renders command results. It never writes tokens: message strings are
// redacted, and callers must not place credentials into data payloads.
type Printer struct {
	stdout  io.Writer
	stderr  io.Writer
	format  Format
	quiet   bool
	noColor bool
}

func NewPrinter(stdout, stderr io.Writer, format Format, quiet, noColor bool) *Printer {
	return &Printer{stdout: stdout, stderr: stderr, format: format, quiet: quiet, noColor: noColor}
}

func (p *Printer) Format() Format { return p.format }

// Success emits the success envelope (json) or the provided table rows (table).
func (p *Printer) Success(command string, data any, meta map[string]any, tableRows [][2]string) error {
	if p.format == FormatJSON {
		return p.writeJSON(Envelope{
			SchemaVersion: SchemaVersion,
			Command:       command,
			OK:            true,
			Data:          data,
			Meta:          normalizeMeta(meta),
		})
	}
	p.Table(tableRows)
	return nil
}

// Failure emits the failure envelope (json mode) or a human error line on
// stderr (table mode) and returns the stable exit code.
func (p *Printer) Failure(command string, err error, meta map[string]any) int {
	ce := clierr.AsCLIError(err)
	if p.format == FormatJSON {
		env := Envelope{
			SchemaVersion: SchemaVersion,
			Command:       command,
			OK:            false,
			Data:          nil,
			Error: &ErrorBody{
				Kind:       string(ce.Kind),
				HTTPStatus: ce.HTTPStatus,
				Code:       ce.Code,
				Message:    Redact(ce.Message),
				Details:    ce.Details,
			},
			Meta: normalizeMeta(meta),
		}
		if werr := p.writeJSON(env); werr != nil {
			fmt.Fprintln(p.stderr, "Error:", Redact(werr.Error()))
		}
	} else {
		fmt.Fprintln(p.stderr, "Error:", Redact(ce.Error()))
	}
	return ce.ExitCode
}

// Progressf writes a progress line to stderr unless --quiet. Never stdout.
func (p *Printer) Progressf(format string, args ...any) {
	if p.quiet {
		return
	}
	fmt.Fprintln(p.stderr, Redact(fmt.Sprintf(format, args...)))
}

// Table renders two-column aligned rows to stdout (table mode only).
func (p *Printer) Table(rows [][2]string) {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(p.stdout, "%-*s  %s\n", width, r[0], Redact(r[1]))
	}
}

func (p *Printer) writeJSON(env Envelope) error {
	enc := json.NewEncoder(p.stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}

func normalizeMeta(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	return meta
}
