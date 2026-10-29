package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/spf13/cobra"
)

const FlagServer = "server"

func RegisterServerSelection(root *cobra.Command) {
	root.PersistentFlags().String(FlagServer, "", "Run the command in the registered server's directory")
	_ = root.RegisterFlagCompletionFunc(FlagServer, completeServerNames)
}

// SelectServer returns a restoration function only when it changes directories.
func SelectServer(cmd *cobra.Command) (func() error, error) {
	if cmd.Name() == "__complete" || cmd.Name() == "__completeNoDesc" {
		return nil, nil
	}
	flags := cmd.Root().PersistentFlags()
	if !flags.Changed(FlagServer) {
		return nil, nil
	}
	name, err := flags.GetString(FlagServer)
	if err != nil {
		return nil, err
	}
	// Native registrations select managed instances, but must not silently
	// shadow a different workspace with the same per-user name.
	if server.ValidateInstanceName(name) == nil {
		inst, readErr := server.ReadInstance(name)
		if readErr != nil {
			return nil, readErr
		}
		if inst != nil {
			registry, err := serverregistry.New()
			if err != nil {
				return nil, err
			}
			entries, err := registry.List()
			if err != nil {
				return nil, err
			}
			managedInfo, err := os.Stat(inst.Root)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if entry.State != serverregistry.StateAvailable || entry.Name != name {
					continue
				}
				info, err := os.Stat(entry.Path)
				if err != nil {
					return nil, err
				}
				if !os.SameFile(managedInfo, info) {
					return nil, fmt.Errorf("server name %q is ambiguous between managed root %s and workspace %s; run in the intended directory or rename the workspace", name, inst.Root, entry.Path)
				}
			}
			original, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			if err := os.Chdir(inst.Root); err != nil {
				return nil, fmt.Errorf("select managed server %q: %w", name, err)
			}
			return func() error { return os.Chdir(original) }, nil
		}
	}
	registry, err := serverregistry.New()
	if err != nil {
		return nil, err
	}
	entry, err := registry.Resolve(name)
	if err != nil {
		return nil, err
	}
	original, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("determine invocation directory: %w", err)
	}
	if err := os.Chdir(entry.Path); err != nil {
		return nil, fmt.Errorf("select server %q: %w", name, err)
	}
	return func() error { return os.Chdir(original) }, nil
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
		if entry.State != serverregistry.StateAvailable || !strings.HasPrefix(entry.Name, prefix) || seen[entry.Name] {
			continue
		}
		seen[entry.Name] = true
		names = append(names, entry.Name+"\t"+entry.Path)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func registerWorkspace(root string, output *Output) {
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
	registry, err := serverregistry.New()
	if err != nil {
		output.Warn(err)
		return
	}
	duplicates, err := registry.Register(root)
	if err != nil {
		output.Warn(fmt.Errorf("server registration failed: %w", err))
		return
	}
	log.Debug("server registration checked", "directory", root)
	for _, duplicate := range duplicates {
		output.Warn(fmt.Errorf(
			"server name %q is also registered at %s; --server cannot select an ambiguous name",
			duplicate.Name,
			duplicate.Path,
		))
	}
}
