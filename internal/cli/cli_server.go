package cli

import (
	"fmt"
	"io"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// serverListJSON is the machine-readable `server list` result: every
// registered entry, including entries whose target cannot serve as a server.
var serverListJSON JSONResult[[]serverregistry.Entry]

// serverRenameJSON is the machine-readable result of one rename.
var serverRenameJSON JSONResult[serverRenameDocument]

type serverRenameDocument struct {
	Previous string   `json:"previous_name"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Warnings []string `json:"warnings,omitempty"`
}

func NewServerCommand(contracts *JSONContracts) *cobra.Command {
	serverListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   help.ServerListShort,
		Args:    cobra.NoArgs,
		RunE:    actionServerList,
	}
	serverRenameCmd := &cobra.Command{
		Use:               "rename <server> <name>",
		Short:             help.ServerRenameShort,
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeServerRenameArguments,
		RunE:              actionServerRename,
	}
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: help.ServerShort,
		Args:  cobra.NoArgs,
	}
	serverListJSON.Bind(contracts, serverListCmd)
	serverRenameJSON.Bind(contracts, serverRenameCmd)
	serverCmd.AddCommand(serverListCmd, serverRenameCmd)
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
func actionServerList(cmd *cobra.Command, _ []string) error {
	output := NewQueryOutput(cmd)
	registry, err := serverregistry.New()
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
	return writeServerList(output.Human(), entries)
}

// writeServerList renders one aligned row per entry. A name column that is
// empty in the registry stays visible as "-", and a problem wraps below the
// entry instead of hiding the path.
func writeServerList(out io.Writer, entries []serverregistry.Entry) error {
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
			value += "\n" + style.Muted(entry.Problem)
		}
		facts = append(facts, terminal.Fact{Key: name, Value: value})
	}
	_, err := fmt.Fprintln(out, terminal.RenderFacts(facts, style.TermWidth()))
	return err
}

// actionServerRename changes the manifest name of one registered server. The
// new name may already be in use; registration reports that ambiguity as a
// warning instead of rejecting the rename.
func actionServerRename(cmd *cobra.Command, args []string) error {
	output := NewOperationOutput(cmd)
	registry, err := serverregistry.New()
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
	previous, err := operation.RenameServer(entry.Path, name)
	if err != nil {
		return err
	}
	registerWorkspace(entry.Path, output)

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
