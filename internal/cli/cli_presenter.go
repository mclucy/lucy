package cli

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"

	"github.com/mclucy/lucy/terminal/progress"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// Output owns a command's streams and optional progress area.
// Documents and their human renderers stay with the command.
type Output struct {
	cmd      *cobra.Command
	json     JSONRendering
	human    io.Writer
	renderer progress.Renderer
	warnings []string
}

// NewQueryOutput sends human results to stdout.
func NewQueryOutput(cmd *cobra.Command) *Output {
	return &Output{cmd: cmd, json: JSONMode(cmd), human: cmd.OutOrStdout()}
}

// NewOperationOutput sends human results to stderr.
func NewOperationOutput(cmd *cobra.Command) *Output {
	return &Output{cmd: cmd, json: JSONMode(cmd), human: cmd.ErrOrStderr()}
}

func (o *Output) JSON() bool { return o.json != JSONNone }

// Human is silent in machine mode, even when a command reports a notice.
func (o *Output) Human() io.Writer {
	if o.JSON() {
		return io.Discard
	}
	return o.human
}

func (o *Output) WriteJSON(value any) error {
	return o.writeJSON(o.cmd.OutOrStdout(), value)
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
		fmt.Fprintln(o.cmd.ErrOrStderr(), style.Note("warning:")+" "+err.Error())
	}
}

func (o *Output) Warnings() []string { return o.warnings }

// Progress starts a renderer only when a command has work to narrate.
func (o *Output) Progress() progress.Renderer {
	if o.renderer == nil {
		o.renderer = progress.Discard()
		if !o.JSON() {
			o.renderer = progress.New(o.cmd.ErrOrStderr())
		}
	}
	return o.renderer
}

func (o *Output) Close() {
	if o.renderer != nil {
		o.renderer.Close()
	}
}
