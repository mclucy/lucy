package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/spf13/cobra"
)

const (
	flagImportDirectoryName = "directory"
	flagImportSHA256Name    = "sha256"
)

// importArchiveSuffixes are removed from a source basename before it becomes
// a directory name.
var importArchiveSuffixes = []string{".mrpack", ".tar.gz", ".zip"}

// importJSON is the machine-readable result of `lucy import`.
var importJSON JSONResult[*operation.ImportResult]

func NewImportCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <source>",
		Short: help.ImportShort,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionImport(cmd, inv, deps, args[0])
		},
	}
	importJSON.Bind(contracts, cmd)
	cmd.Flags().StringP(
		flagImportDirectoryName,
		"d",
		"",
		"Directory to create the workspace in (default: the current directory when empty, otherwise a directory named after the source)",
	)
	cmd.Flags().String(
		flagInitNameName,
		"",
		"Server name to record (default: the target directory name)",
	)
	cmd.Flags().String(
		flagInitMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	cmd.Flags().Bool(
		flagInitNoCommentsName,
		false,
		"Write generated manifests and configuration without explanatory comments",
	)
	cmd.Flags().String(
		flagImportSHA256Name,
		"",
		"Expected SHA-256 of the source archive, as 64 hexadecimal characters",
	)
	cmd.Flags().String(
		flagInitCoreName,
		"",
		"Expect this server product; no match fails and lists the observed runtimes",
	)
	cmd.Flags().String(
		flagInitCoreVersionName,
		"",
		"Expect this exact server product version",
	)
	cmd.Flags().String(
		flagInitMinecraftName,
		"",
		"Expect this exact Minecraft version",
	)
	return cmd
}

func actionImport(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	source string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
	opts, err := importOptions(cmd, inv, source)
	if err != nil {
		return err
	}
	return importWorkspace(ctx, cmd, inv, deps, opts)
}

// importOptions turns the command line into operation options. Local sources
// resolve against the invocation directory; URLs pass through unchanged.
func importOptions(
	cmd *cobra.Command,
	inv *Invocation,
	source string,
) (operation.ImportOptions, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return operation.ImportOptions{}, errors.New("source must not be empty")
	}
	if _, ok := importURL(source); !ok {
		source = resolveInvocationPath(inv.Directory, source)
	}
	target, err := importTarget(
		inv,
		flagStringInit(cmd, flagImportDirectoryName),
		source,
	)
	if err != nil {
		return operation.ImportOptions{}, err
	}
	name := flagStringInit(cmd, flagInitNameName)
	if cmd.Flags().Changed(flagInitNameName) {
		if _, err := document.NormalizeName(name); err != nil {
			return operation.ImportOptions{}, fmt.Errorf(
				"--%s: %w",
				flagInitNameName,
				err,
			)
		}
	} else {
		name = filepath.Base(target)
	}
	sha := strings.ToLower(
		strings.TrimSpace(flagStringInit(cmd, flagImportSHA256Name)),
	)
	if cmd.Flags().Changed(flagImportSHA256Name) && !validImportSHA256(sha) {
		return operation.ImportOptions{}, fmt.Errorf(
			"--%s expects %d hexadecimal characters",
			flagImportSHA256Name,
			hex.EncodedLen(sha256.Size),
		)
	}
	expectation, err := initExpectation(cmd)
	if err != nil {
		return operation.ImportOptions{}, err
	}
	return operation.ImportOptions{
		Source:      source,
		Directory:   target,
		Name:        name,
		MCDRVersion: flagStringInit(cmd, flagInitMCDRVersionName),
		NoComments:  flagBoolInit(cmd, flagInitNoCommentsName),
		SHA256:      sha,
		Expectation: expectation,
	}, nil
}

// importTarget chooses the workspace directory. An explicit directory wins.
// Without one, an empty or absent invocation directory is used in place; a
// non-empty one receives a new directory named after the source.
func importTarget(inv *Invocation, explicit, source string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return resolveInvocationPath(inv.Directory, explicit), nil
	}
	empty, err := directoryIsEmpty(inv.Directory)
	if err != nil {
		return "", err
	}
	if empty {
		return filepath.Clean(inv.Directory), nil
	}
	slug, err := importSlug(source)
	if err != nil {
		return "", err
	}
	return filepath.Join(inv.Directory, slug), nil
}

func directoryIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", dir, err)
	}
	return len(entries) == 0, nil
}

// importSlug derives a directory name from a source basename.
func importSlug(source string) (string, error) {
	base := importSourceBase(source)
	for _, suffix := range importArchiveSuffixes {
		if len(base) > len(suffix) &&
			strings.EqualFold(base[len(base)-len(suffix):], suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	slug := sanitizeImportSlug(base)
	if slug == "" {
		return "", fmt.Errorf(
			"cannot derive a directory name from %s; pass --%s",
			source,
			flagImportDirectoryName,
		)
	}
	return slug, nil
}

func importSourceBase(source string) string {
	if parsed, ok := importURL(source); ok {
		base := path.Base(parsed.Path)
		if base == "/" || base == "." {
			return ""
		}
		return base
	}
	return filepath.Base(source)
}

// sanitizeImportSlug lowercases name and joins its ASCII letter and digit runs
// with hyphens, so the result is always a single safe path segment.
func sanitizeImportSlug(name string) string {
	var builder strings.Builder
	separator := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if separator && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(r)
			separator = false
			continue
		}
		separator = true
	}
	return builder.String()
}

// importURL reports whether source is an http or https URL.
func importURL(source string) (*url.URL, bool) {
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" {
		return nil, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false
	}
	return parsed, true
}

func validImportSHA256(value string) bool {
	if len(value) != hex.EncodedLen(sha256.Size) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func importWorkspace(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	opts operation.ImportOptions,
) error {
	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	opts.PromptRuntime = func(observed discovery.Result) (*discovery.Installation, error) {
		output.Close()
		return promptRuntime(ctx, cmd, inv, observed)
	}
	result, err := deps.Operations.Import(ctx, opts, deps.Network)
	if err != nil {
		return output.Failure(err)
	}
	output.Close()
	for _, warning := range result.Warnings {
		output.Warn(warning)
	}
	reportDiscoveryFindings(&result.Discovery, output.Output)
	if output.JSON() {
		return importJSON.Write(output.Output, result)
	}
	writeImportReport(output.Human(), inv, result)
	return nil
}

func writeImportReport(
	w io.Writer,
	inv *Invocation,
	result *operation.ImportResult,
) {
	fmt.Fprintf(
		w,
		"Imported %s into %s\n",
		result.Name,
		displayCreatePath(result.Workspace, inv.Directory),
	)
	fmt.Fprintf(w, "  Server: %s\n", importServerLabel(result))
	fmt.Fprintf(
		w,
		"  Files: %d server, %d client files skipped\n",
		result.ServerFiles,
		result.SkippedClient,
	)
	fmt.Fprintf(w, "  Managed: %s\n", packageCountLabel(result.ManagedPackages))
	if len(result.Unmanaged) > 0 {
		fmt.Fprintf(w, "  Unmanaged: %d files\n", len(result.Unmanaged))
		for _, file := range result.Unmanaged {
			fmt.Fprintf(w, "    %s\n", file)
		}
	}
	if len(result.Review) > 0 {
		fmt.Fprintln(w, "  Review:")
		for _, line := range result.Review {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
}

func importServerLabel(result *operation.ImportResult) string {
	label := result.Product
	if result.CoreVersion != "" {
		label += " " + result.CoreVersion
	}
	return label + " for Minecraft " + result.Minecraft
}
