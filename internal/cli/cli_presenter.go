package cli

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"

	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/progress"
	"github.com/spf13/cobra"
)

// Output owns a command's streams and optional progress area.
// Documents and their human renderers stay with the command.
type Output struct {
	cmd      *cobra.Command
	inv      *Invocation
	json     JSONRendering
	human    io.Writer
	renderer progress.Renderer
	warnings []string
}

// NewQueryOutput sends human results to stdout.
func NewQueryOutput(cmd *cobra.Command, inv *Invocation) *Output {
	return &Output{cmd: cmd, inv: inv, json: JSONMode(cmd), human: cmd.OutOrStdout()}
}

// NewOperationOutput sends human results to stderr.
func NewOperationOutput(cmd *cobra.Command, inv *Invocation) *Output {
	return &Output{cmd: cmd, inv: inv, json: JSONMode(cmd), human: cmd.ErrOrStderr()}
}

func (o *Output) JSON() bool { return o.json != JSONNone }

// Human is silent in machine mode, even when a command reports a notice.
func (o *Output) Human() io.Writer {
	if o.JSON() {
		return io.Discard
	}
	return o.human
}

func (o *Output) writeJSON(w io.Writer, value any) error {
	var options []json.Options
	if o.json != JSONCompact {
		options = []json.Options{jsontext.Multiline(true), jsontext.WithIndent("  ")}
	}
	data, err := json.Marshal(value, options...)
	if err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// Warn records warnings for the result document and reports them in human mode.
func (o *Output) Warn(err error) {
	if err == nil {
		return
	}
	o.warnings = append(o.warnings, err.Error())
	if !o.JSON() {
		o.Close()
		fmt.Fprintln(
			o.cmd.ErrOrStderr(),
			o.inv.Theme.Note("warning:")+" "+err.Error(),
		)
	}
}

func (o *Output) Warnings() []string { return o.warnings }

// Info reports an informational human notice. Machine output carries the
// same fact in its document instead.
func (o *Output) Info(message string) {
	if o.JSON() {
		return
	}
	report.New(o.cmd.ErrOrStderr(), o.inv.Theme).Info(message)
}

// Error reports an error-level human notice that does not fail the command.
func (o *Output) Error(err error) {
	if err == nil || o.JSON() {
		return
	}
	o.Close()
	report.New(o.cmd.ErrOrStderr(), o.inv.Theme).Error(err)
}

// Progress starts a renderer only when a command has work to narrate.
func (o *Output) Progress() progress.Renderer {
	if o.renderer == nil {
		o.renderer = o.newProgress()
	}
	return o.renderer
}

// RestartProgress rebuilds the renderer after a command suspended it, for
// example while a package manager owned the terminal.
func (o *Output) RestartProgress() {
	o.renderer = o.newProgress()
}

func (o *Output) newProgress() progress.Renderer {
	if o.JSON() {
		return progress.Discard()
	}
	return progress.New(progress.Options{
		Writer:      o.cmd.ErrOrStderr(),
		Theme:       o.inv.Theme,
		Terminal:    o.inv.Stderr,
		Interactive: o.inv.Interactive,
	})
}

func (o *Output) Close() {
	if o.renderer != nil {
		o.renderer.Close()
	}
}
