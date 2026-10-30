package cli

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// storeObject is one stored object as the command presents it.
type storeObject struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// storeListJSON is the machine-readable `store list` result: every stored
// object in digest order.
var storeListJSON JSONResult[[]storeObject]

// storeGCJSON is the machine-readable result of one collection.
var storeGCJSON JSONResult[artifactstore.Report]

func NewStoreCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	storeListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   help.StoreListShort,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionStoreList(cmd, inv, args)
		},
	}
	storeGCCmd := &cobra.Command{
		Use:   "gc",
		Short: help.StoreGCShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionStoreGC(cmd, inv, deps, args)
		},
	}
	storeCmd := &cobra.Command{
		Use:   "store",
		Short: help.StoreShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	MarkHelpOnly(storeCmd)
	storeListJSON.Bind(contracts, storeListCmd)
	storeGCJSON.Bind(contracts, storeGCCmd)
	storeCmd.AddCommand(storeListCmd, storeGCCmd)
	return storeCmd
}

func actionStoreList(
	cmd *cobra.Command,
	inv *Invocation,
	_ []string,
) error {
	output := NewQueryOutput(cmd, inv)
	store, err := artifactstore.Open()
	if err != nil {
		return err
	}
	defer store.Close()
	objects := make([]cas.Object, 0)
	if err := store.Walk(func(object cas.Object) error {
		objects = append(objects, object)
		return nil
	}); err != nil {
		return fmt.Errorf("list artifact store: %w", err)
	}
	slices.SortFunc(objects, func(a, b cas.Object) int {
		return cmp.Compare(a.Digest.String(), b.Digest.String())
	})
	if output.JSON() {
		presented := make([]storeObject, 0, len(objects))
		for _, object := range objects {
			presented = append(presented, storeObject{
				Digest: object.Digest.String(),
				Size:   object.Size,
			})
		}
		return storeListJSON.Write(output, presented)
	}
	if len(objects) == 0 {
		_, err := fmt.Fprintln(output.Human(), "Artifact store is empty")
		return err
	}
	var total int64
	for _, object := range objects {
		total += object.Size
	}
	facts := make([]terminal.Fact, 0, len(objects)+1)
	facts = append(facts, terminal.Fact{
		Key:   "Store",
		Value: fmt.Sprintf("%d objects, %s", len(objects), style.FormatBytesBinary(total)),
	})
	for _, object := range objects {
		facts = append(facts, terminal.Fact{
			Key:   object.Digest.String(),
			Value: style.FormatBytesBinary(object.Size),
		})
	}
	_, err = fmt.Fprintln(
		output.Human(),
		terminal.RenderFacts(facts, inv.Stdout.Width, inv.Theme),
	)
	return err
}

func actionStoreGC(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	output := NewOperationOutput(cmd, inv)
	registry, err := deps.ServerRegistry()
	if err != nil {
		return err
	}
	store, err := artifactstore.Open()
	if err != nil {
		return err
	}
	defer store.Close()
	report, err := store.GC(registry)
	if err != nil {
		return err
	}
	if output.JSON() {
		return storeGCJSON.Write(output, report)
	}
	if report.Removed == 0 {
		_, err := fmt.Fprintln(output.Human(), "Artifact store has no unreferenced objects")
		return err
	}
	_, err = fmt.Fprintf(
		output.Human(),
		"removed %d objects, freed up %s of space\n",
		report.Removed,
		style.FormatBytesBinary(report.Bytes),
	)
	return err
}
