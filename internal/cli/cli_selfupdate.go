package cli

import (
	"fmt"
	"runtime"

	"github.com/mclucy/lucy/internal/selfupdate"
	"github.com/spf13/cobra"
)

const (
	flagSelfUpdateCheck = "check"
	flagSelfUpdateForce = "force"
)

// BuildInfo identifies the running binary for commands that reason about
// releases. The root wiring supplies it from linker flags.
type BuildInfo struct {
	Version  string
	Commit   string
	Official bool
}

var selfUpdateJSON JSONResult[selfUpdateDocument]

type selfUpdateDocument struct {
	Schema          int      `json:"schema"`
	Kind            string   `json:"kind"`
	CurrentVersion  string   `json:"current_version"`
	CurrentCommit   string   `json:"current_commit,omitempty"`
	LatestVersion   string   `json:"latest_version,omitempty"`
	LatestChannel   string   `json:"latest_channel,omitempty"`
	LatestCommit    string   `json:"latest_commit,omitempty"`
	UpdateAvailable bool     `json:"update_available"`
	Action          string   `json:"action"`
	Asset           string   `json:"asset,omitempty"`
	Checksum        string   `json:"checksum,omitempty"`
	Executable      string   `json:"executable,omitempty"`
	ReleaseURL      string   `json:"release_url,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// NewSelfUpdateCommand replaces the running binary from the published release
// manifest.
func NewSelfUpdateCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
	build BuildInfo,
) *cobra.Command {
	command := &cobra.Command{
		Use:   "self-update",
		Short: "Update the lucy binary",
		Long: "Update the lucy binary in place from the published release manifest. " +
			"Installations managed by Homebrew, Go, Docker, or Nix update through their manager.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionSelfUpdate(cmd, inv, deps, build)
		},
	}
	selfUpdateJSON.Bind(contracts, command)
	command.Flags().Bool(
		flagSelfUpdateCheck,
		false,
		"Report the available release without changing the binary",
	)
	command.Flags().Bool(
		flagSelfUpdateForce,
		false,
		"Reinstall the selected release over a non-release build or the same version",
	)
	return command
}

func actionSelfUpdate(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	build BuildInfo,
) error {
	check, _ := cmd.Flags().GetBool(flagSelfUpdateCheck)
	force, _ := cmd.Flags().GetBool(flagSelfUpdateForce)
	if check && force {
		return fmt.Errorf("--%s cannot be combined with --%s", flagSelfUpdateCheck, flagSelfUpdateForce)
	}

	ctx, stop := CommandContext(cmd)
	defer stop()

	output := NewOperationOutput(cmd, inv)
	defer output.Close()
	service := selfupdate.New(deps.Network, inv.Logger)
	options := selfupdate.Options{
		Version:  build.Version,
		Commit:   build.Commit,
		Official: build.Official,
		Force:    force,
	}

	if check {
		result, err := service.Check(ctx, options)
		if err != nil {
			return err
		}
		return writeSelfUpdate(output, result, true)
	}

	preview, err := service.Check(ctx, options)
	if err != nil {
		return err
	}
	if !preview.UpdateAvailable && !force {
		return writeSelfUpdate(output, preview, false)
	}
	if !output.JSON() {
		fmt.Fprintf(
			output.Human(),
			"Updating lucy %s → %s (%s/%s)\n",
			selfUpdateVersionLabel(preview.CurrentVersion),
			preview.LatestVersion,
			runtime.GOOS,
			runtime.GOARCH,
		)
	}

	progress := output.Progress()
	options.Progress = progress.Download
	result, err := service.Apply(ctx, options)
	progress.DownloadDone()
	if err != nil {
		return err
	}
	return writeSelfUpdate(output, result, false)
}

func writeSelfUpdate(output *Output, result *selfupdate.Result, check bool) error {
	action := result.Action
	switch {
	case check:
		action = "checked"
	case action != "updated":
		action = "up-to-date"
	}

	if output.JSON() {
		return selfUpdateJSON.Write(output, selfUpdateDocument{
			Schema:          1,
			Kind:            "self-update",
			CurrentVersion:  result.CurrentVersion,
			CurrentCommit:   result.CurrentCommit,
			LatestVersion:   result.LatestVersion,
			LatestChannel:   result.LatestChannel,
			LatestCommit:    result.LatestCommit,
			UpdateAvailable: result.UpdateAvailable,
			Action:          action,
			Asset:           result.Asset,
			Checksum:        result.Checksum,
			Executable:      result.Executable,
			ReleaseURL:      result.ReleaseURL,
			Warnings:        result.Notes,
		})
	}

	switch {
	case result.Action == "updated":
		fmt.Fprintf(
			output.Human(),
			"%s Updated lucy %s → %s\n",
			output.inv.Theme.Success("✓"),
			selfUpdateVersionLabel(result.CurrentVersion),
			result.LatestVersion,
		)
		fmt.Fprintln(output.Human(), "  "+output.inv.Theme.Muted(result.Executable))
	case result.UpdateAvailable:
		fmt.Fprintf(
			output.Human(),
			"lucy %s is available (current: %s).\n",
			result.LatestVersion,
			selfUpdateVersionLabel(result.CurrentVersion),
		)
	default:
		fmt.Fprintf(
			output.Human(),
			"lucy %s is up to date.\n",
			selfUpdateVersionLabel(result.CurrentVersion),
		)
	}
	for _, note := range result.Notes {
		output.Info(note)
	}
	return nil
}

func selfUpdateVersionLabel(version string) string {
	if version == "" {
		return "unknown"
	}
	return version
}
