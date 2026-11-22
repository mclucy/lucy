package cmd

import (
	"math"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The argument shape is bounded so one invocation cannot grow the event
// without limit. Names are long flag names or positional slots. Values are
// recorded only when the declaration opted into a closed semantic domain and
// the parsed value passes that domain's grammar.
const (
	telemetryMaxArguments     = 32
	telemetryMaxArgumentName  = 64
	telemetryMaxArgumentValue = 64
	telemetryMaxSafeInteger   = 1<<53 - 1

	telemetryKindBoolean  = "boolean"
	telemetryKindNumber   = "number"
	telemetryKindSelector = "selector"
	telemetryKindVersion  = "version"
	telemetryKindText     = "text"
	telemetryKindList     = "list"
)

// telemetryArgument is one replayable fragment of the command line. Value is
// present only for a boolean, a bounded number, or a value drawn from a closed
// selector/version vocabulary.
type telemetryArgument struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value any    `json:"value,omitempty"`
}

// telemetrySafeSelectors is a closed vocabulary of product, provider, channel,
// and policy choices. Matching is exact: an arbitrary string that happens to
// contain one of these words is still recorded without its value.
var telemetrySafeSelectors = map[string]struct{}{
	"vanilla":    {},
	"paper":      {},
	"fabric":     {},
	"folia":      {},
	"purpur":     {},
	"forge":      {},
	"neoforge":   {},
	"quilt":      {},
	"spigot":     {},
	"bukkit":     {},
	"velocity":   {},
	"waterfall":  {},
	"modrinth":   {},
	"curseforge": {},
	"hangar":     {},
	"github":     {},
	"release":    {},
	"snapshot":   {},
	"beta":       {},
	"alpha":      {},
	"stable":     {},
	"auto":       {},
	"latest":     {},
	"none":       {},
	"always":     {},
	"never":      {},
	"off":        {},
	"on":         {},
}

// telemetryArguments projects the parsed command line into bounded arguments.
// It never reads raw argv: flag names and changed state come from Cobra, and
// positional and flag values are recorded only through annotated semantic
// declarations. Callers must skip it after a failed parse because Cobra leaves
// parsed state from an earlier execution in place.
func telemetryArguments(command *cobra.Command) []telemetryArgument {
	if command == nil || !command.Flags().Parsed() {
		return nil
	}
	flags := telemetrySetFlags(command)
	arguments := make([]telemetryArgument, 0, min(len(flags)+len(command.Flags().Args()), telemetryMaxArguments))
	for _, flag := range flags {
		if len(arguments) >= telemetryMaxArguments {
			return arguments
		}
		if argument, ok := telemetryFlagArgument(flag); ok {
			arguments = append(arguments, argument)
		}
	}
	for index, value := range command.Flags().Args() {
		if len(arguments) >= telemetryMaxArguments {
			break
		}
		position := index + 1
		name := "arg-" + strconv.Itoa(position)
		arguments = append(arguments, telemetryPositionalArgument(command, position, name, value))
	}
	return arguments
}

// telemetrySetFlags reads the parsed flag set after Cobra has merged inherited
// and root persistent flags. Visit yields only set flags, in name order.
func telemetrySetFlags(command *cobra.Command) []*pflag.Flag {
	var flags []*pflag.Flag
	command.Flags().Visit(func(flag *pflag.Flag) {
		if flag == nil || flag.Hidden {
			return
		}
		flags = append(flags, flag)
	})
	return flags
}

func telemetryFlagArgument(flag *pflag.Flag) (telemetryArgument, bool) {
	if flag == nil || flag.Value == nil || !telemetryValidArgumentName(flag.Name) {
		return telemetryArgument{}, false
	}
	kind, value := telemetryFlagValue(flag)
	return telemetryArgument{Name: flag.Name, Kind: kind, Value: value}, true
}

func telemetryFlagValue(flag *pflag.Flag) (string, any) {
	switch flag.Value.Type() {
	case "bool":
		value, err := strconv.ParseBool(flag.Value.String())
		if err != nil {
			return telemetryKindBoolean, nil
		}
		return telemetryKindBoolean, value
	case "int", "int8", "int16", "int32", "int64", "count":
		if value, err := strconv.ParseInt(flag.Value.String(), 10, 64); err == nil &&
			telemetrySafeNumber(float64(value)) {
			return telemetryKindNumber, value
		}
		return telemetryKindNumber, nil
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if value, err := strconv.ParseUint(flag.Value.String(), 10, 64); err == nil &&
			value <= telemetryMaxSafeInteger {
			return telemetryKindNumber, int64(value)
		}
		return telemetryKindNumber, nil
	case "float32", "float64":
		if value, err := strconv.ParseFloat(flag.Value.String(), 64); err == nil &&
			telemetrySafeNumber(value) {
			return telemetryKindNumber, value
		}
		return telemetryKindNumber, nil
	case "duration":
		// A duration is numeric but has no lossless JSON-number spelling in
		// this schema, so the kind is retained without a value.
		return telemetryKindNumber, nil
	case "string":
		if kind, value, ok := telemetryAnnotatedValue(cli.TelemetryFlagArgumentKind(flag), flag.Value.String()); ok {
			return kind, value
		}
		return telemetryKindText, nil
	case "stringSlice", "stringArray", "intSlice", "int64Slice",
		"uintSlice", "boolSlice", "float32Slice", "float64Slice",
		"stringToString":
		return telemetryKindList, nil
	default:
		return telemetryKindText, nil
	}
}

func telemetryPositionalArgument(command *cobra.Command, position int, name, value string) telemetryArgument {
	if kind, safe, ok := telemetryAnnotatedValue(
		cli.TelemetryPositionArgumentKind(command, position),
		value,
	); ok {
		return telemetryArgument{Name: name, Kind: kind, Value: safe}
	}
	return telemetryArgument{Name: name, Kind: telemetryKindText}
}

// telemetryAnnotatedValue validates a value only against the semantic domain
// its declaration opted into. An unmarked declaration is free text and never
// records its value, even when the spelling looks like a selector or version.
func telemetryAnnotatedValue(annotation, value string) (string, any, bool) {
	if len(value) == 0 || len(value) > telemetryMaxArgumentValue {
		return "", nil, false
	}
	switch annotation {
	case cli.TelemetryArgumentSelector:
		if safe, ok := telemetrySelectorValue(value); ok {
			return telemetryKindSelector, safe, true
		}
	case cli.TelemetryArgumentVersion:
		if telemetrySafeVersion(value) {
			return telemetryKindVersion, value, true
		}
		if _, ok := telemetrySafeSelectors[value]; ok {
			return telemetryKindSelector, value, true
		}
	}
	return "", nil, false
}

// telemetrySelectorValue accepts a closed selector or a selector@version pair
// whose components each pass their own grammar. The pair is recorded as a
// selector because the product choice supplies its meaning.
func telemetrySelectorValue(value string) (string, bool) {
	if _, ok := telemetrySafeSelectors[value]; ok {
		return value, true
	}
	selector, version, found := strings.Cut(value, "@")
	if !found || strings.Contains(version, "@") {
		return "", false
	}
	if _, ok := telemetrySafeSelectors[selector]; !ok || !telemetrySafeVersion(version) {
		return "", false
	}
	return value, true
}

// telemetrySafeVersion recognizes Mojang-style numeric segments and snapshot
// identifiers. Segments are ASCII digits only and no segment exceeds four
// digits; the snapshot form is exactly [0-9]{2}w[0-9]{2}[a-z].
func telemetrySafeVersion(value string) bool {
	if len(value) == 0 || len(value) > 19 {
		return false
	}
	if len(value) == 6 &&
		telemetryDigit(value[0]) && telemetryDigit(value[1]) &&
		value[2] == 'w' &&
		telemetryDigit(value[3]) && telemetryDigit(value[4]) &&
		value[5] >= 'a' && value[5] <= 'z' {
		return true
	}
	segments := 1
	digits := 0
	for index := range len(value) {
		switch {
		case value[index] == '.':
			if digits == 0 || digits > 4 {
				return false
			}
			segments++
			if segments > 4 {
				return false
			}
			digits = 0
		case telemetryDigit(value[index]):
			digits++
			if digits > 4 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0
}

func telemetryDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func telemetrySafeNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		math.Abs(value) <= telemetryMaxSafeInteger
}

// telemetryValidArgumentName enforces the wire name grammar without a list, so
// a newly registered flag appears in the shape as soon as it is parsed.
func telemetryValidArgumentName(name string) bool {
	if len(name) == 0 || len(name) > telemetryMaxArgumentName {
		return false
	}
	if !telemetryNameCharacter(name[0], true) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !telemetryNameCharacter(name[index], false) {
			return false
		}
	}
	return true
}

func telemetryNameCharacter(character byte, first bool) bool {
	if (character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9') {
		return true
	}
	return character == '-' && !first
}
