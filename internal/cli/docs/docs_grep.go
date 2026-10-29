package docs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
)

type grepOptions struct {
	ignoreCase        bool
	lineNumber        bool
	filesWithMatches  bool
	filesWithoutMatch bool
	count             bool
	invert            bool
	fixed             bool
	word              bool
	wholeLine         bool
	patterns          []string
	before            int
	after             int
	context           int
	noFilename        bool
	withFilename      bool
	quiet             bool
}

func newGrepCommand() *cobra.Command {
	var opts grepOptions
	cmd := &cobra.Command{
		Use:   "grep <pattern> [page...]",
		Short: help.DocsGrepShort,
		Long:  help.DocsGrepLong,
		Args: func(_ *cobra.Command, args []string) error {
			if len(opts.patterns) == 0 && len(args) == 0 {
				return cli.ExitWith(2, errors.New("missing pattern"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrep(cmd, args, &opts)
		},
	}
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cli.ExitWith(2, err)
	})
	// Grep uses -h for --no-filename, so reserve the long flag for help.
	cmd.Flags().Bool("help", false, "help for grep")
	cmd.Flags().BoolVarP(
		&opts.ignoreCase,
		"ignore-case",
		"i",
		false,
		"Ignore case distinctions",
	)
	cmd.Flags().BoolVarP(
		&opts.lineNumber,
		"line-number",
		"n",
		false,
		"Prefix each output line with its line number",
	)
	cmd.Flags().BoolVarP(
		&opts.filesWithMatches,
		"files-with-matches",
		"l",
		false,
		"Print only page slugs with matches",
	)
	cmd.Flags().BoolVarP(
		&opts.filesWithoutMatch,
		"files-without-match",
		"L",
		false,
		"Print only page slugs without matches",
	)
	cmd.Flags().BoolVarP(
		&opts.count,
		"count",
		"c",
		false,
		"Print a count of matching lines per page",
	)
	cmd.Flags().BoolVarP(
		&opts.invert,
		"invert-match",
		"v",
		false,
		"Select non-matching lines",
	)
	cmd.Flags().BoolVarP(
		&opts.fixed,
		"fixed-strings",
		"F",
		false,
		"Interpret patterns as fixed strings",
	)
	cmd.Flags().BoolVarP(
		&opts.word,
		"word-regexp",
		"w",
		false,
		"Match whole words only",
	)
	cmd.Flags().BoolVarP(
		&opts.wholeLine,
		"line-regexp",
		"x",
		false,
		"Match whole lines only",
	)
	cmd.Flags().StringArrayVarP(
		&opts.patterns,
		"regexp",
		"e",
		nil,
		"Use PATTERN for matching; repeatable",
	)
	cmd.Flags().IntVarP(
		&opts.after,
		"after-context",
		"A",
		0,
		"Print NUM lines after a match",
	)
	cmd.Flags().IntVarP(
		&opts.before,
		"before-context",
		"B",
		0,
		"Print NUM lines before a match",
	)
	cmd.Flags().IntVarP(
		&opts.context,
		"context",
		"C",
		0,
		"Print NUM lines before and after a match",
	)
	cmd.Flags().BoolVarP(
		&opts.noFilename,
		"no-filename",
		"h",
		false,
		"Do not prefix output with page slugs",
	)
	cmd.Flags().BoolVarP(
		&opts.withFilename,
		"with-filename",
		"H",
		false,
		"Prefix output with page slugs",
	)
	cmd.Flags().BoolVarP(
		&opts.quiet,
		"quiet",
		"q",
		false,
		"Suppress output; exit 0 on the first match",
	)
	return cmd
}

func runGrep(cmd *cobra.Command, args []string, opts *grepOptions) error {
	if opts.filesWithMatches && opts.filesWithoutMatch {
		return cli.ExitWith(2, errors.New("-l and -L are mutually exclusive"))
	}
	if opts.before < 0 || opts.after < 0 || opts.context < 0 {
		return cli.ExitWith(2, errors.New("context values must not be negative"))
	}
	patterns := opts.patterns
	pageArgs := args
	if len(patterns) == 0 {
		patterns = []string{args[0]}
		pageArgs = args[1:]
	}
	match, err := newLineMatcher(patterns, opts)
	if err != nil {
		return cli.ExitWith(2, fmt.Errorf("invalid pattern: %w", err))
	}
	pages, err := loadPages()
	if err != nil {
		return cli.ExitWith(2, err)
	}
	selected := pages
	if len(pageArgs) > 0 {
		selected = make([]page, 0, len(pageArgs))
		for _, raw := range pageArgs {
			slug, err := normalizeSlug(raw)
			if err != nil {
				return cli.ExitWith(2, err)
			}
			selectedPage, ok := findPage(pages, slug)
			if !ok {
				return cli.ExitWith(2, fmt.Errorf("unknown documentation page %q", raw))
			}
			selected = append(selected, selectedPage)
		}
	}
	showFilename := opts.withFilename ||
		(!opts.noFilename && len(selected) > 1)
	out := cmd.OutOrStdout()
	success := false
	for _, selectedPage := range selected {
		lines := splitLines(selectedPage.content)
		matched := make([]bool, len(lines))
		anyMatch := false
		for index, line := range lines {
			hit := match(line)
			if opts.invert {
				hit = !hit
			}
			matched[index] = hit
			if hit {
				anyMatch = true
			}
		}
		if opts.quiet {
			if anyMatch {
				return nil
			}
			continue
		}
		switch {
		case opts.filesWithMatches:
			if anyMatch {
				success = true
				if _, err := fmt.Fprintln(out, selectedPage.slug); err != nil {
					return err
				}
			}
		case opts.filesWithoutMatch:
			if !anyMatch {
				success = true
				if _, err := fmt.Fprintln(out, selectedPage.slug); err != nil {
					return err
				}
			}
		case opts.count:
			if anyMatch {
				success = true
			}
			if err := writeCount(out, selectedPage.slug, matched, showFilename); err != nil {
				return err
			}
		default:
			if anyMatch {
				success = true
				if err := writeMatches(
					out,
					selectedPage.slug,
					lines,
					matched,
					opts,
					showFilename,
				); err != nil {
					return err
				}
			}
		}
	}
	if success {
		return nil
	}
	return cli.SilentExit(1)
}

func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	text := strings.TrimSuffix(string(content), "\n")
	lines := strings.Split(text, "\n")
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}
