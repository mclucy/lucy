package cli

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/mclucy/lucy/report"
	"github.com/spf13/cobra"
)

func NewSchemaCommand(contracts *JSONContracts) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "schema [COMMAND...]",
		Short:  "Describe a command's JSON result",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewQueryOutput(cmd)
			if !output.JSON() {
				output.SetJSONRendering(JSONPretty)
			}
			failure, _ := cmd.Flags().GetBool("error")
			if failure {
				if len(args) != 0 {
					return fmt.Errorf("--error describes shared failures and does not accept a command path")
				}
				schema := reflectJSONSchema([]reflect.Type{reflect.TypeFor[ErrorDocument]()})
				schema.Title = "Lucy JSON failure"
				return (JSONResult[*jsonschema.Schema]{}).Write(output, schema)
			}
			target := cmd.Root()
			if len(args) != 0 {
				found, remaining, err := cmd.Root().Find(args)
				if err != nil || len(remaining) != 0 {
					return &report.Detail{
						Title: fmt.Sprintf("unknown command path %q", strings.Join(args, " ")),
						Code:  CodeInvalidInvocation,
						Hint:  "Run lucy schema to list supported command paths.",
					}
				}
				target = found
			}
			if len(contracts.results[target]) == 0 {
				paths := contracts.Paths(target)
				if target == cmd.Root() || len(paths) != 0 {
					return (JSONResult[[]string]{}).Write(output, paths)
				}
				return &report.Detail{
					Title: fmt.Sprintf("%s does not provide a JSON result schema", target.CommandPath()),
					Code:  "usage.unsupported_output",
					Hint:  "Run lucy schema to list supported command paths.",
				}
			}
			schema, err := contracts.Schema(target)
			if err != nil {
				return err
			}
			return (JSONResult[*jsonschema.Schema]{}).Write(output, schema)
		},
	}
	cmd.Flags().Bool("error", false, "Describe the shared JSON failure document on stderr")
	contracts.Native(cmd)
	return cmd
}
