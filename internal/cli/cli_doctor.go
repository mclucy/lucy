package cli

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// Live states reported by the file tracking service. Doctor classifies them
// here; the wording of every state stays owned by the service.
const (
	fileStateMissing  = "missing"
	fileStateModified = "modified"
	fileStateError    = "error"
)

// doctorReport is the machine-readable doctor outcome.
type doctorReport struct {
	Warnings []filetrack.Diagnostic `json:"warnings,omitempty"`
	Failures []filetrack.Diagnostic `json:"failures,omitempty"`
	// Java is the Java runtime check; nil when the workspace has no lock.
	Java *javaruntime.Check `json:"java,omitempty"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: help.DoctorShort,
	Args:  cobra.NoArgs,
	RunE:  actionDoctor,
}

func NewDoctorCommand() *cobra.Command {
	return doctorCmd
}

// actionDoctor reports tracked file and Java runtime diagnostics without any
// network access: live observations come from the manifest or lock
// declarations only.
func actionDoctor(cmd *cobra.Command, _ []string) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	files, err := filetrack.DeclaredFiles(root)
	if err != nil {
		return err
	}

	statuses, err := filetrack.Inspect(root, files)
	if err != nil {
		return err
	}

	warnings, failures := classifyFileStatuses(statuses)

	javaCheck, err := doctorJava(cmd, root)
	if err != nil {
		return err
	}

	switch JSONMode(cmd) {
	case JSONCompact:
		if err := writeDoctorReport(cmd, true, warnings, failures, javaCheck); err != nil {
			return err
		}
	case JSONPretty:
		if err := writeDoctorReport(cmd, false, warnings, failures, javaCheck); err != nil {
			return err
		}
	default:
		for _, warning := range warnings {
			report.Warn(fmt.Errorf("%s: %s", warning.Path, warning.Message))
		}
		if len(statuses) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No tracked files declared.")
		}
		writeDoctorJava(cmd.OutOrStdout(), javaCheck)
		for _, warning := range javaWarnings(javaCheck) {
			report.Warn(warning)
		}
	}

	if len(failures) > 0 {
		return fileFailureError(failures)
	}
	return nil
}

// classifyFileStatuses turns live observations into warnings a user can act on
// and failures that must stop the command. No network request happens here.
func classifyFileStatuses(
	statuses []filetrack.Status,
) (warnings, failures []filetrack.Diagnostic) {
	for _, status := range statuses {
		switch {
		case status.State == fileStateError:
			reason := status.Error
			if reason == "" {
				reason = "tracked file cannot be inspected"
			}
			failures = append(
				failures,
				filetrack.Diagnostic{Path: status.Path, Message: reason},
			)
		case status.State == fileStateMissing:
			warnings = append(
				warnings,
				filetrack.Diagnostic{
					Path:    status.Path,
					Message: "tracked file is missing",
				},
			)
		case status.Immutable && status.State == fileStateModified:
			warnings = append(
				warnings,
				filetrack.Diagnostic{
					Path: status.Path,
					Message: "immutable file differs from its accepted contents; " +
						"run lucy file update to accept them or " +
						"lucy file recover to restore them",
				},
			)
		}
	}
	return warnings, failures
}

func fileFailureError(failures []filetrack.Diagnostic) error {
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reasons = append(reasons, fmt.Sprintf("%s: %s", failure.Path, failure.Message))
	}
	return fmt.Errorf(
		"%d tracked file(s) cannot be inspected: %s",
		len(failures),
		strings.Join(reasons, "; "),
	)
}

func writeDoctorReport(
	cmd *cobra.Command,
	compact bool,
	warnings []filetrack.Diagnostic,
	failures []filetrack.Diagnostic,
	javaCheck *javaruntime.Check,
) error {
	report := doctorReport{Warnings: warnings, Failures: failures, Java: javaCheck}

	var (
		data []byte
		err  error
	)
	if compact {
		data, err = json.Marshal(report)
	} else {
		data, err = json.Marshal(
			report,
			jsontext.Multiline(true),
			jsontext.WithIndent("  "),
		)
	}
	if err != nil {
		return err
	}

	if _, err := cmd.OutOrStdout().Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// doctorJava selects the Java runtime of the locked server. A workspace
// without a lock has no Java requirement to check.
func doctorJava(cmd *cobra.Command, root string) (*javaruntime.Check, error) {
	requirement, err := lockedJavaRequirement(root)
	if err != nil || requirement == nil {
		return nil, err
	}
	ctx, stop := commandContext(cmd)
	defer stop()
	return javaruntime.Select(ctx, root, *requirement)
}

func writeDoctorJava(out io.Writer, check *javaruntime.Check) {
	if check == nil {
		return
	}
	if check.Runtime == nil {
		fmt.Fprintf(out, "Java: none (the server asks for Java %d)\n", check.Target)
		return
	}
	status := style.Success("✓")
	if !check.Satisfied() {
		status = style.Failure("✗")
	}
	fmt.Fprintf(
		out,
		"%s Java %s · %s · %s\n",
		status,
		check.Runtime.Version,
		check.Runtime.Build(),
		style.Muted(check.Runtime.Home),
	)
}
