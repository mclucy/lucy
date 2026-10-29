package cli

import (
	"fmt"
	"io"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// serverListJSON is the machine-readable `server list` result: every
// registered entry, including entries whose target cannot serve as a server.
var serverListJSON JSONResult[[]serverregistry.Entry]

// serverForgetJSON is the machine-readable result of forgetting one entry.
var serverForgetJSON JSONResult[serverForgetDocument]

// serverRenameJSON is the machine-readable result of one rename.
var serverRenameJSON JSONResult[serverRenameDocument]

type serverForgetDocument struct {
	Path string `json:"path"`
}

type serverRenameDocument struct {
	Previous string   `json:"previous_name"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Warnings []string `json:"warnings,omitempty"`
}

func NewServerCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	serverListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   help.ServerListShort,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerList(cmd, inv, deps, args)
		},
	}
	serverForgetCmd := &cobra.Command{
		Use:   "forget <path>",
		Short: help.ServerForgetShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerForget(cmd, inv, deps, args)
		},
	}
	serverRenameCmd := &cobra.Command{
		Use:               "rename <server> <name>",
		Short:             help.ServerRenameShort,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeServerRenameArguments,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerRename(cmd, inv, deps, args)
		},
	}
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: help.ServerShort,
		Args:  cobra.NoArgs,
	}
	serverListJSON.Bind(contracts, serverListCmd)
	serverForgetJSON.Bind(contracts, serverForgetCmd)
	serverRenameJSON.Bind(contracts, serverRenameCmd)
	serverCmd.AddCommand(serverListCmd, serverForgetCmd, serverRenameCmd)
	return serverCmd
}

func completeServerRenameArguments(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeServerNames(cmd, args, toComplete)
}

// actionServerList reports the registry as it is. Unavailable targets stay in
// the listing with their state and problem instead of being pruned or hidden.
func actionServerList(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	output := NewQueryOutput(cmd, inv)
	registry, err := deps.ServerRegistry()
	if err != nil {
		return err
	}
	entries, err := registry.List()
	if err != nil {
		return err
	}
	if output.JSON() {
		if entries == nil {
			entries = []serverregistry.Entry{}
		}
		return serverListJSON.Write(output, entries)
	}
	return writeServerList(output.Human(), entries, inv.Theme, inv.Stdout.Width)
}

// writeServerList renders one aligned row per entry. A name column that is
// empty in the registry stays visible as "-", and a problem wraps below the
// entry instead of hiding the path.
func writeServerList(
	out io.Writer,
	entries []serverregistry.Entry,
	theme style.Theme,
	width int,
) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(out, "No servers registered")
		return err
	}
	facts := make([]terminal.Fact, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name
		if name == "" {
			name = "-"
		}
		value := entry.State + "  " + entry.Path
		if entry.Problem != "" {
			value += "\n" + theme.Muted(entry.Problem)
		}
		facts = append(facts, terminal.Fact{Key: name, Value: value})
	}
	_, err := fmt.Fprintln(out, terminal.RenderFacts(facts, width, theme))
	return err
}

// actionServerForget removes a registry entry whose recorded server directory
// is gone. The registry decides whether the target is confirmed missing; the
// command reports the forgotten path and never deletes server data.
func actionServerForget(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
	output := NewOperationOutput(cmd, inv)
	registry, err := deps.ServerRegistry()
	if err != nil {
		return err
	}
	entry, err := registry.Forget(args[0])
	if err != nil {
		return err
	}
	if output.JSON() {
		return serverForgetJSON.Write(output, serverForgetDocument{Path: entry.Path})
	}
	_, err = fmt.Fprintf(output.Human(), "forgot server %s\n", entry.Path)
	return err
}

// actionServerRename changes the manifest name of one registered server. The
// new name may already be in use; registration reports that ambiguity as a
// warning instead of rejecting the rename.
func actionServerRename(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
	output := NewOperationOutput(cmd, inv)
	registry, err := deps.ServerRegistry()
	if err != nil {
		return err
	}
	entry, err := registry.Resolve(args[0])
	if err != nil {
		return err
	}
	name, err := document.NormalizeName(args[1])
	if err != nil {
		return err
	}
	previous, err := deps.Operations.RenameServer(entry.Path, name)
	if err != nil {
		return err
	}
	registerWorkspace(entry.Path, output, inv, deps)

	result := serverRenameDocument{
		Previous: previous,
		Name:     name,
		Path:     entry.Path,
		Warnings: output.Warnings(),
	}
	if output.JSON() {
		return serverRenameJSON.Write(output, result)
	}
	out := output.Human()
	switch {
	case previous == "":
		_, err = fmt.Fprintf(out, "named server %s\n", name)
	case previous == name:
		_, err = fmt.Fprintf(out, "server is already named %s\n", name)
	default:
		_, err = fmt.Fprintf(out, "renamed %s to %s\n", previous, name)
	}
	return err
}
