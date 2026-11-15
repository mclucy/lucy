package cli

import (
	"bytes"
	"io"

	"github.com/mclucy/lucy/terminal/pager"
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
// redirect, or test sees the same bytes it would without a pager.
func PageDocument(
	cmd *cobra.Command,
	inv *Invocation,
	writer io.Writer,
	render func(io.Writer) error,
) error {
	if !inv.Interactive {
		return render(writer)
	}
	var document bytes.Buffer
	if err := render(&document); err != nil {
		return err
	}
	handled, pagerErr := pager.Show(pager.Options{
		Title:    cmd.CommandPath(),
		Text:     document.String(),
		Input:    cmd.InOrStdin(),
		Output:   writer,
		Terminal: inv.Stdout,
		Theme:    inv.Theme,
		Mode:     inv.Pager,
	})
	if handled && pagerErr == nil {
		return nil
	}
	if _, err := io.WriteString(writer, document.String()); err != nil {
		return err
	}
	return pagerErr
}

// Document renders a command's human result through the invocation's pager.
func (o *Output) Document(render func(io.Writer) error) error {
	if o.JSON() {
		return nil
	}
	return PageDocument(o.cmd, o.inv, o.human, render)
}
