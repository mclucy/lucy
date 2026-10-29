package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/spf13/cobra"
)

const FlagServer = "server"

func RegisterServerSelection(root *cobra.Command) {
	root.PersistentFlags().String(FlagServer, "", "Run the command in the registered server's directory")
	_ = root.RegisterFlagCompletionFunc(FlagServer, completeServerNames)
}

// ResolveServer applies the parsed --server selection to inv. It resolves the
// registered name to its absolute directory; the process working directory is
// never changed. Completion commands are not server selections.
func ResolveServer(
	cmd *cobra.Command,
	registry *serverregistry.Registry,
	inv *Invocation,
) error {
	if cmd.Name() == "__complete" || cmd.Name() == "__completeNoDesc" {
		return nil
	}
	flags := cmd.Root().PersistentFlags()
	if !flags.Changed(FlagServer) {
		return nil
	}
	name, err := flags.GetString(FlagServer)
	if err != nil {
		return err
	}
	if registry == nil {
		return fmt.Errorf("select server %q: server registry is unavailable", name)
	}
	entry, err := registry.Resolve(name)
	if err != nil {
		return err
	}
	inv.Directory = entry.Path
	inv.Logger.Debug(
		"server selected",
		"name", name,
		"directory", entry.Path,
	)
	return nil
}

func completeServerNames(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	registry, err := serverregistry.New()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	entries, err := registry.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := make(map[string]bool)
	var names []string
	for _, entry := range entries {
		if entry.State != serverregistry.StateAvailable || entry.Name == "" || !strings.HasPrefix(entry.Name, prefix) || seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		names = append(names, entry.Name+"\t"+entry.Path)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func registerWorkspace(
	root string,
	output *Output,
	inv *Invocation,
	deps *Dependencies,
) {
	if err := workspacewrite.SafePath(root, ".lucy/"+workspacewrite.LockFileName); err != nil {
		output.Warn(fmt.Errorf("server registration failed: %w", err))
		return
	}
	// Do not recreate .lucy if deinit removed it after the operation returned.
	writer, err := fsutil.TryLockFile(filepath.Join(root, ".lucy", workspacewrite.LockFileName))
	if err != nil {
		output.Warn(fmt.Errorf("server registration failed: %w", err))
		return
	}
	defer func() {
		if err := writer.Close(); err != nil {
			output.Warn(fmt.Errorf("release server registration lock: %w", err))
		}
	}()
	registry, err := deps.ServerRegistry()
	if err != nil {
		output.Warn(fmt.Errorf("server registration skipped: %w", err))
		return
	}
	duplicates, err := registry.Register(root)
	if err != nil {
		output.Warn(fmt.Errorf("server registration failed: %w", err))
		return
	}
	inv.Logger.Debug("server registration checked", "directory", root)
	for _, duplicate := range duplicates {
		output.Warn(fmt.Errorf(
			"server name %q is also registered at %s; --server cannot select an ambiguous name",
			duplicate.Name,
			duplicate.Path,
		))
	}
}
