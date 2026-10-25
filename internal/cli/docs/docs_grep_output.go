package docs

import (
	"fmt"
	"io"
)

func writeCount(out io.Writer, slug string, matched []bool, showFilename bool) error {
	count := 0
	for _, hit := range matched {
		if hit {
			count++
		}
	}
	if showFilename {
		_, err := fmt.Fprintf(out, "%s:%d\n", slug, count)
		return err
	}
	_, err := fmt.Fprintf(out, "%d\n", count)
	return err
}

type matchPrinter struct {
	out            io.Writer
	slug           string
	showFilename   bool
	showLineNumber bool
}

func (p *matchPrinter) line(text string, number int, match bool) error {
	separator := "-"
	if match {
		separator = ":"
	}
	switch {
	case p.showFilename && p.showLineNumber:
		_, err := fmt.Fprintf(
			p.out,
			"%s%s%d%s%s\n",
			p.slug,
			separator,
			number,
			separator,
			text,
		)
		return err
	case p.showFilename:
		_, err := fmt.Fprintf(p.out, "%s%s%s\n", p.slug, separator, text)
		return err
	case p.showLineNumber:
		_, err := fmt.Fprintf(p.out, "%d%s%s\n", number, separator, text)
		return err
	default:
		_, err := fmt.Fprintln(p.out, text)
		return err
	}
}

func writeMatches(
	out io.Writer,
	slug string,
	lines []string,
	matched []bool,
	opts *grepOptions,
	showFilename bool,
) error {
	before := opts.before
	after := opts.after
	if opts.context > 0 {
		before = max(before, opts.context)
		after = max(after, opts.context)
	}
	printer := &matchPrinter{
		out:            out,
		slug:           slug,
		showFilename:   showFilename,
		showLineNumber: opts.lineNumber,
	}
	if before == 0 && after == 0 {
		for index, hit := range matched {
			if !hit {
				continue
			}
			if err := printer.line(lines[index], index+1, true); err != nil {
				return err
			}
		}
		return nil
	}
	type lineRange struct {
		start int
		end   int
	}
	ranges := make([]lineRange, 0)
	for index, hit := range matched {
		if !hit {
			continue
		}
		start := max(0, index-before)
		end := min(len(lines)-1, index+after)
		if len(ranges) > 0 && start <= ranges[len(ranges)-1].end+1 {
			if end > ranges[len(ranges)-1].end {
				ranges[len(ranges)-1].end = end
			}
			continue
		}
		ranges = append(
			ranges,
			lineRange{
				start: start,
				end:   end,
			},
		)
	}
	for rangeIndex, group := range ranges {
		if rangeIndex > 0 {
			if _, err := fmt.Fprintln(out, "--"); err != nil {
				return err
			}
		}
		for lineIndex := group.start; lineIndex <= group.end; lineIndex++ {
			if err := printer.line(
				lines[lineIndex],
				lineIndex+1,
				matched[lineIndex],
			); err != nil {
				return err
			}
		}
	}
	return nil
}
