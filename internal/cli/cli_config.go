package cli

import (
	"fmt"
	"io"

	"github.com/mclucy/lucy/config"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	flagConfigGlobalName     = "global"
	flagConfigForceName      = "force"
	flagConfigNoCommentsName = "no-comments"
)

// configValuesJSON, configJavaJSON, configGitHubJSON, configCurseForgeJSON,
// and configStringJSON are the shapes `lucy config get` prints: a whole layer,
// a section, or a scalar leaf.
var (
	configValuesJSON     JSONResult[*config.Values]
	configJavaJSON       JSONResult[config.Java]
	configGitHubJSON     JSONResult[config.GitHub]
	configCurseForgeJSON JSONResult[config.CurseForge]
	configStringJSON     JSONResult[string]
)

// configWriteJSON is the machine-readable result of `lucy config set` and
// `lucy config create`.
var configWriteJSON JSONResult[*config.WriteResult]

func NewConfigCommand(
	contracts *JSONContracts,
	inv *Invocation,
	_ *Dependencies,
) *cobra.Command {
	configGetCmd := &cobra.Command{
		Use:   "get [key]",
		Short: help.ConfigGetShort,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionConfigGet(cmd, inv, args)
		},
	}
	configSetCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: help.ConfigSetShort,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionConfigSet(cmd, inv, args)
		},
	}
	configCreateCmd := &cobra.Command{
		Use:   "create",
		Short: help.ConfigCreateShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return actionConfigCreate(cmd, inv)
		},
	}
	configCmd := &cobra.Command{
		Use:   "config",
		Short: help.ConfigShort,
	}
	configValuesJSON.Bind(contracts, configGetCmd)
	configJavaJSON.Bind(contracts, configGetCmd)
	configGitHubJSON.Bind(contracts, configGetCmd)
	configCurseForgeJSON.Bind(contracts, configGetCmd)
	configStringJSON.Bind(contracts, configGetCmd)
	configWriteJSON.Bind(contracts, configSetCmd)
	configWriteJSON.Bind(contracts, configCreateCmd)
	configCmd.PersistentFlags().Bool(
		flagConfigGlobalName,
		false,
		"Use the global configuration instead of the workspace configuration",
	)
	configCreateCmd.Flags().BoolP(
		flagConfigForceName,
		"f",
		false,
		"Overwrite an existing configuration file",
	)
	configCreateCmd.Flags().Bool(
		flagConfigNoCommentsName,
		false,
		"Write the configuration without explanatory comments",
	)
	configCmd.AddCommand(configGetCmd, configSetCmd, configCreateCmd)
	return configCmd
}

func actionConfigGet(
	cmd *cobra.Command,
	inv *Invocation,
	args []string,
) error {
	service, err := newConfigService(inv)
	if err != nil {
		return err
	}
	key := ""
	if len(args) == 1 {
		key = args[0]
	}
	value, err := service.Get(key, configScope(cmd))
	if err != nil {
		return err
	}
	if !config.Secret(key) {
		value = config.Redacted(value)
	}
	output := NewQueryOutput(cmd, inv)
	if output.JSON() {
		return writeConfigJSON(output, value)
	}
	return writeConfigValue(output.Human(), key, value)
}

// writeConfigJSON writes the shape the selected key produced.
func writeConfigJSON(output *Output, value any) error {
	switch value := value.(type) {
	case *config.Values:
		return configValuesJSON.Write(output, value)
	case config.Java:
		return configJavaJSON.Write(output, value)
	case config.GitHub:
		return configGitHubJSON.Write(output, value)
	case config.CurseForge:
		return configCurseForgeJSON.Write(output, value)
	case string:
		return configStringJSON.Write(output, value)
	default:
		return fmt.Errorf("unsupported config value type %T", value)
	}
}

func actionConfigSet(
	cmd *cobra.Command,
	inv *Invocation,
	args []string,
) error {
	service, err := newConfigService(inv)
	if err != nil {
		return err
	}
	key, value := args[0], args[1]
	result, err := service.Set(key, value, configScope(cmd))
	if err != nil {
		return err
	}
	inv.Logger.Info(
		"configuration updated",
		"path", result.Path,
		"key", key,
		"scope", configScopeLabel(cmd),
		"created", result.Created,
	)
	output := NewOperationOutput(cmd, inv)
	if output.JSON() {
		return configWriteJSON.Write(output, result)
	}
	out := output.Human()
	if result.Created {
		if _, err := fmt.Fprintf(out, "Created %s\n", result.Path); err != nil {
			return err
		}
	}
	shown := value
	if config.Secret(key) && shown != "" {
		shown = config.RedactedValue
	}
	_, err = fmt.Fprintf(out, "Set %s to %s\n", key, shown)
	return err
}

func actionConfigCreate(cmd *cobra.Command, inv *Invocation) error {
	service, err := newConfigService(inv)
	if err != nil {
		return err
	}
	force, err := cmd.Flags().GetBool(flagConfigForceName)
	if err != nil {
		return err
	}
	noComments, err := cmd.Flags().GetBool(flagConfigNoCommentsName)
	if err != nil {
		return err
	}
	result, err := service.Create(
		configScope(cmd),
		config.CreateOptions{Force: force, NoComments: noComments},
	)
	if err != nil {
		return err
	}
	inv.Logger.Info(
		"configuration written",
		"path", result.Path,
		"scope", configScopeLabel(cmd),
		"created", result.Created,
	)
	output := NewOperationOutput(cmd, inv)
	if output.JSON() {
		return configWriteJSON.Write(output, result)
	}
	verb := "Wrote"
	if result.Created {
		verb = "Created"
	}
	_, err = fmt.Fprintf(output.Human(), "%s %s\n", verb, result.Path)
	return err
}

func newConfigService(inv *Invocation) (*config.Service, error) {
	return config.New(inv.Directory, inv.Logger)
}

func configScopeLabel(cmd *cobra.Command) string {
	if configScope(cmd) == config.GlobalScope {
		return "global"
	}
	return "workspace"
}

func configScope(cmd *cobra.Command) config.Scope {
	global, _ := cmd.Flags().GetBool(flagConfigGlobalName)
	if global {
		return config.GlobalScope
	}
	return config.WorkspaceScope
}

// writeConfigValue renders the value of a key for people: scalar values print
// verbatim, while a section prints as YAML. The whole effective configuration
// also prints as YAML.
func writeConfigValue(out io.Writer, key string, value any) error {
	if key != "" {
		switch value := value.(type) {
		case nil:
			_, err := fmt.Fprintln(out)
			return err
		case string:
			_, err := fmt.Fprintln(out, value)
			return err
		}
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	_, err = out.Write(data)
	return err
}
