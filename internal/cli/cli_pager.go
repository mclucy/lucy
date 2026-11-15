package cli

import (
	"bytes"
	"io"

	"github.com/mclucy/lucy/terminal/pager"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// FlagPager selects when long human output goes through the pager.
const FlagPager = "pager"

// AddPagerFlag registers the pager mode on the root command. A bare --pager
// requests paging regardless of document length.
func AddPagerFlag(root *cobra.Command, inv *Invocation) {
	root.PersistentFlags().Var(
		pagerModeValue{mode: &inv.Pager},
		FlagPager,
		"Page long human output: auto, always, or never; --pager alone means always",
	)
	root.PersistentFlags().Lookup(FlagPager).NoOptDefVal = pager.Always.String()
	_ = root.RegisterFlagCompletionFunc(
		FlagPager,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{"auto", "always", "never"}, cobra.ShellCompDirectiveNoFileComp
		},
	)
}

type pagerModeValue struct {
	mode *pager.Mode
}

func (v pagerModeValue) String() string {
	if v.mode == nil {
		return pager.Auto.String()
	}
	return v.mode.String()
}

func (v pagerModeValue) Set(raw string) error {
	mode, err := pager.ParseMode(raw)
	if err != nil {
		return err
	}
	*v.mode = mode
	return nil
}

func (v pagerModeValue) Type() string { return "mode" }

// PageDocument renders one human document, paging it when the invocation and
// terminal allow it. Rendering completes before the pager starts, so a pipe,
// redirect, or test sees the same bytes it would without a pager. While the
// pager is open, a resize re-renders the document at the new geometry.
func PageDocument(
	cmd *cobra.Command,
	inv *Invocation,
	writer io.Writer,
	render func(io.Writer, style.Terminal) error,
) error {
	if !inv.Interactive || inv.Pager == pager.Never {
		return render(writer, inv.Stdout)
	}
	renderAt := func(width, height int) (string, error) {
		var document bytes.Buffer
		screen := inv.Stdout
		screen.Width = width
		screen.Height = height
		if err := render(&document, screen); err != nil {
			return "", err
		}
		return document.String(), nil
	}
	initial, err := renderAt(inv.Stdout.Width, inv.Stdout.Height)
	if err != nil {
		return err
	}
	handled, pagerErr := pager.Show(pager.Options{
		Title:    cmd.CommandPath(),
		Text:     initial,
		Render:   renderAt,
		Input:    cmd.InOrStdin(),
		Output:   writer,
		Terminal: inv.Stdout,
		Theme:    inv.Theme,
		Mode:     inv.Pager,
	})
	if handled && pagerErr == nil {
		return nil
	}
	if _, err := io.WriteString(writer, initial); err != nil {
		return err
	}
	return pagerErr
}

// Document renders a command's human result through the invocation's pager.
func (o *Output) Document(render func(io.Writer, style.Terminal) error) error {
	if o.JSON() {
		return nil
	}
	return PageDocument(o.cmd, o.inv, o.human, render)
}
