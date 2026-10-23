package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/mclucy/lucy/javainstall"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/progress"
	"github.com/mclucy/lucy/terminal/style"
)

// provisionJava returns the install hook for --java. It installs the first
// usable plan, most native first. A Java runtime that the local settings
// select is the user's choice, so the hook installs nothing then. Every
// installation method needs the network, so the hook fails when offline.
func (o *installOutput) provisionJava(host javainstall.Host, offline bool) func(context.Context, javaruntime.Requirement, *javaruntime.Check) error {
	return func(ctx context.Context, _ javaruntime.Requirement, check *javaruntime.Check) error {
		if check.Source == javaruntime.SourceLocal {
			log.Info(
				"java installation skipped",
				"reason", manifest.LocalFilename+" selects the Java runtime",
				"home", check.LocalHome,
			)
			return nil
		}
		if offline {
			return &report.Detail{
				Title: "cannot install Java while offline",
				Lines: []string{fmt.Sprintf(
					"no installed runtime meets the requirement for Java %d, and every installation method needs network access",
					check.Target,
				)},
				Hint: "Run lucy install --java without --offline, or select a runtime with lucy java use --path.",
			}
		}
		request := javainstall.Request{Major: check.Target, Distribution: check.Distribution}
		plans, skipped, err := javainstall.Plans(ctx, request, host)
		if err != nil {
			return err
		}
		if len(plans) == 0 {
			return noJavaPlanDetail(request, skipped)
		}
		plan := plans[0]
		log.Info(
			"java install plan selected",
			"method", plan.Method,
			"package", plan.Package,
			"alternatives", len(plans)-1,
			"skipped", len(skipped),
		)
		return o.suspend(func(w io.Writer) error {
			fmt.Fprintf(
				w,
				"%s Java %d (%s) with %s\n",
				style.Accent("Installing"),
				request.Major,
				javaDistributionLabel(plan.Distribution),
				plan.Method,
			)
			for _, line := range plan.Commands() {
				fmt.Fprintln(w, "  "+style.Muted(line))
			}
			if plan.Method == javainstall.MethodDownload {
				for _, s := range skipped {
					fmt.Fprintln(w, "  "+style.Muted(skipText(s)))
				}
			}
			return javainstall.Execute(ctx, plan, host, w)
		})
	}
}

// suspend stops the progress area while run owns the terminal, so a package
// manager can prompt for a password, then starts a new progress area.
func (o *installOutput) suspend(run func(io.Writer) error) error {
	o.renderer.Close()
	defer func() {
		if o.json == JSONNone {
			o.renderer = progress.New(o.cmd.ErrOrStderr())
		}
	}()
	return run(o.Human())
}

func noJavaPlanDetail(request javainstall.Request, skipped []javainstall.Skipped) error {
	detail := &report.Detail{
		Title: fmt.Sprintf(
			"no way to install Java %d (%s) is available here",
			request.Major,
			javaDistributionLabel(request.Distribution),
		),
	}
	for _, s := range skipped {
		detail.Lines = append(detail.Lines, skipText(s))
	}
	if len(skipped) == 0 {
		detail.Hint = "Install the Java runtime manually, then run lucy java use --path <home>."
	}
	return detail
}

func skipText(s javainstall.Skipped) string {
	offer := fmt.Sprintf("%s offers %s", s.Plan.Method, s.Plan.Package)
	switch s.Reason {
	case javainstall.SkipNeedsRepositoryConsent:
		return fmt.Sprintf(
			"%s from %s (%s); rerun with --%s to add the repository",
			offer,
			s.Plan.Repository.Name,
			s.Plan.Repository.URL,
			flagInstallJavaRepoName,
		)
	case javainstall.SkipNeedsTerminal:
		return offer + ", but it asks for confirmation; rerun in an interactive terminal"
	case javainstall.SkipNeedsRoot:
		return offer + ", but it needs root; rerun as root or with sudo access"
	default:
		return offer
	}
}
