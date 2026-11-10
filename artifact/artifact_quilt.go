package artifact

import (
	"archive/zip"
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/version"
)

const quiltModPath = "quilt.mod.json"

func archiveHasEntry(zipRdr *zip.Reader, name string) bool {
	for _, file := range zipRdr.File {
		if file.Name == name {
			return true
		}
	}
	return false
}

// validateQuiltDescriptor rejects schema versions this reader cannot
// interpret instead of guessing at a newer layout.
func validateQuiltDescriptor(descriptor *quiltModFile) error {
	if descriptor.SchemaVersion != 1 {
		return fmt.Errorf(
			"unsupported Quilt schema_version %d",
			descriptor.SchemaVersion,
		)
	}
	if descriptor.Loader.ID == "" {
		return fmt.Errorf("Quilt descriptor is missing quilt_loader.id")
	}
	return nil
}

// quiltModFile is the schema_version 1 quilt.mod.json document. The schema
// keeps dependency, license, and contributor values in several shapes, so the
// flexible fields decode into any and are interpreted through the helpers in
// this file.
type quiltModFile struct {
	SchemaVersion int         `json:"schema_version"`
	Loader        quiltLoader `json:"quilt_loader"`
	Minecraft     struct {
		Environment string `json:"environment"`
	} `json:"minecraft"`
}

type quiltLoader struct {
	Group    string                 `json:"group"`
	ID       string                 `json:"id"`
	Version  string                 `json:"version"`
	Provides []quiltProvided        `json:"provides"`
	Jars     []string               `json:"jars"`
	Depends  []quiltDependencyEntry `json:"depends"`
	Breaks   []quiltDependencyEntry `json:"breaks"`
	Metadata *quiltMetadata         `json:"metadata"`
}

type quiltMetadata struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Contributors map[string]any    `json:"contributors"`
	Contact      map[string]string `json:"contact"`
	License      quiltLicense      `json:"license"`
}

// quiltProvided accepts the string shorthand and the object form of a
// provides entry. Version is empty when the entry omits it, which means the
// providing module's own version applies.
type quiltProvided struct {
	ID      string
	Version version.Version
}

func (p *quiltProvided) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		p.ID = id
		return nil
	}
	var object struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt provides entry: %w", err)
	}
	if object.ID == "" {
		return fmt.Errorf("Quilt provides entry is missing an id")
	}
	p.ID = object.ID
	p.Version = version.Version(object.Version)
	return nil
}

// quiltLicense collapses the string, array, and object license forms into the
// single display string catalog.Metadata carries.
type quiltLicense string

func (l *quiltLicense) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*l = quiltLicense(single)
		return nil
	}
	var many []quiltLicense
	if err := json.Unmarshal(data, &many); err == nil {
		parts := make([]string, 0, len(many))
		for _, license := range many {
			if license != "" {
				parts = append(parts, string(license))
			}
		}
		*l = quiltLicense(strings.Join(parts, ", "))
		return nil
	}
	var object struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt license: %w", err)
	}
	switch {
	case object.ID != "":
		*l = quiltLicense(object.ID)
	case object.Name != "":
		*l = quiltLicense(object.Name)
	}
	return nil
}

// quiltDependencyEntry models one element of a Quilt depends or breaks array.
// An element is a mod id string, a dependency object, or an array of
// dependency objects, and the two latter shapes may nest.
type quiltDependencyEntry struct {
	ID       string
	Versions any
	Optional bool
	// Environment is the Minecraft game provider option. A dependency that
	// applies only to the physical client does not constrain a server.
	Environment string
	Unless      *quiltDependencyEntry
	Elements    []quiltDependencyEntry
	array       bool
}

func (e *quiltDependencyEntry) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		e.ID = id
		return nil
	}
	var elements []quiltDependencyEntry
	if err := json.Unmarshal(data, &elements); err == nil {
		e.Elements = elements
		e.array = true
		return nil
	}
	var object struct {
		ID          string                `json:"id"`
		Versions    any                   `json:"versions"`
		Optional    bool                  `json:"optional"`
		Environment string                `json:"environment"`
		Unless      *quiltDependencyEntry `json:"unless"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt dependency entry: %w", err)
	}
	if object.ID == "" {
		return fmt.Errorf("Quilt dependency object is missing an id")
	}
	e.ID = object.ID
	e.Versions = object.Versions
	e.Optional = object.Optional
	e.Environment = object.Environment
	e.Unless = object.Unless
	return nil
}

// alternatives flattens nested dependency arrays into the alternatives their
// enclosing entry offers. depends arrays match when any alternative matches.
func (e quiltDependencyEntry) alternatives() []quiltDependencyEntry {
	if !e.array {
		return []quiltDependencyEntry{e}
	}
	var alternatives []quiltDependencyEntry
	for _, element := range e.Elements {
		alternatives = append(alternatives, element.alternatives()...)
	}
	return alternatives
}

// serverAlternatives drops alternatives that apply only to the physical
// client. The Minecraft game provider adds the environment option, and a
// dedicated server does not enforce client-only dependencies.
func (e quiltDependencyEntry) serverAlternatives() []quiltDependencyEntry {
	alternatives := e.alternatives()
	applicable := alternatives[:0]
	for _, alternative := range alternatives {
		if quiltDependencyAppliesToServer(alternative) {
			applicable = append(applicable, alternative)
		}
	}
	return applicable
}

func quiltDependencyAppliesToServer(entry quiltDependencyEntry) bool {
	switch strings.ToLower(strings.TrimSpace(entry.Environment)) {
	case "", "*", "any", "both", "server", "dedicated_server":
		return true
	default:
		return false
	}
}

// parseQuiltVersions interprets the versions field of a dependency object.
// Strings use the Quilt specifier grammar, which differs from Fabric for a
// bare version: 1.0.0 means >=1.0.0 and <2.0.0, while only =1.0.0 is exact.
// Arrays are the deprecated any-of form, and objects select any or all of
// their nested specifiers.
func parseQuiltVersions(raw any) (version.Expr, error) {
	switch value := raw.(type) {
	case nil:
		return nil, nil
	case string:
		return parseQuiltVersionSpecifier(value)
	case []any:
		return parseQuiltVersionContainer(value, true)
	case map[string]any:
		if len(value) != 1 {
			return nil, fmt.Errorf(
				"Quilt versions object must hold exactly one of any or all",
			)
		}
		if anyRaw, ok := value["any"]; ok {
			return parseQuiltVersionContainer(anyRaw, true)
		}
		if allRaw, ok := value["all"]; ok {
			return parseQuiltVersionContainer(allRaw, false)
		}
		return nil, fmt.Errorf(
			"Quilt versions object must hold exactly one of any or all",
		)
	default:
		return nil, fmt.Errorf("invalid Quilt versions value %T", raw)
	}
}

func parseQuiltVersionContainer(raw any, anyMode bool) (version.Expr, error) {
	items, ok := raw.([]any)
	if !ok {
		items = []any{raw}
	}
	var result version.Expr
	for _, item := range items {
		expr, err := parseQuiltVersions(item)
		if err != nil {
			return nil, err
		}
		if anyMode {
			if len(expr) == 0 {
				return nil, nil
			}
			result = append(result, expr...)
			continue
		}
		result = andVersionExpr(result, expr)
	}
	return result, nil
}

// parseQuiltVersionSpecifier parses one Quilt version specifier. Space and
// comma separated predicates are conjunctive, while || alternatives are
// disjunctive.
func parseQuiltVersionSpecifier(raw string) (version.Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil, nil
	}
	engine, err := version.NewEngine(version.DialectFabric)
	if err != nil {
		return nil, err
	}

	alternatives := strings.Split(raw, "||")
	result := make(version.Expr, 0, len(alternatives))
	for _, alternative := range alternatives {
		alternative = strings.TrimSpace(alternative)
		if alternative == "" {
			return nil, fmt.Errorf("empty alternative in Quilt version %q", raw)
		}
		if strings.Contains(alternative, " - ") {
			expr, err := engine.ParseConstraint(alternative)
			if err != nil {
				return nil, err
			}
			if len(expr) == 0 {
				return nil, nil
			}
			result = append(result, expr...)
			continue
		}

		clause := version.Expr(nil)
		for _, token := range strings.FieldsFunc(
			alternative,
			func(r rune) bool { return r == ' ' || r == '\t' || r == ',' },
		) {
			expr, err := parseQuiltVersionToken(engine, token)
			if err != nil {
				return nil, err
			}
			clause = andVersionExpr(clause, expr)
		}
		if len(clause) == 0 {
			return nil, nil
		}
		result = append(result, clause...)
	}
	return result, nil
}

func parseQuiltVersionToken(engine version.Engine, token string) (version.Expr, error) {
	token = strings.TrimSpace(token)
	if token == "" || token == "*" {
		return nil, nil
	}
	if quiltTokenHasOperator(token) || quiltVersionHasWildcard(token) {
		return engine.ParseConstraint(token)
	}

	lower, err := engine.ParseVersion(version.Version(token))
	if err != nil {
		return nil, modelQuiltVersionToken(token, err)
	}
	semantic, ok := lower.(*version.SemverVersion)
	if !ok {
		return nil, fmt.Errorf(
			"Quilt version %q is not a semantic version",
			token,
		)
	}
	// Quilt reads a bare version as the compatible range below the next major
	// version. The trailing hyphen marks the exclusive upper boundary so the
	// next major's prereleases stay outside the range.
	upper, err := engine.ParseVersion(
		version.Version(fmt.Sprintf("%d.0.0-", semantic.Major()+1)),
	)
	if err != nil {
		return nil, modelQuiltVersionToken(token, err)
	}
	return version.Expr{{
		{Value: lower, Operator: version.OpGte},
		{Value: upper, Operator: version.OpLt},
	}}, nil
}

func modelQuiltVersionToken(token string, err error) error {
	return fmt.Errorf("invalid Quilt version %q: %w", token, err)
}

func quiltTokenHasOperator(token string) bool {
	for _, prefix := range []string{
		">=", "<=", "!=", "==", ">", "<", "=", "^", "~",
	} {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

func quiltVersionHasWildcard(token string) bool {
	core := token
	if index := strings.IndexByte(core, '+'); index >= 0 {
		core = core[:index]
	}
	if index := strings.IndexByte(core, '-'); index >= 0 {
		core = core[:index]
	}
	for _, part := range strings.Split(core, ".") {
		switch part {
		case "x", "X", "*":
			return true
		}
	}
	return false
}

// andVersionExpr conjoins two disjunctive normal form expressions. A nil
// expression is unconstrained and therefore scores no terms.
func andVersionExpr(left, right version.Expr) version.Expr {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	result := make(version.Expr, 0, len(left)*len(right))
	for _, leftClause := range left {
		for _, rightClause := range right {
			clause := make(
				[]version.SubExpr,
				0,
				len(leftClause)+len(rightClause),
			)
			clause = append(clause, leftClause...)
			clause = append(clause, rightClause...)
			result = append(result, clause)
		}
	}
	return result
}

// renderVersionExpr writes an expression back as a Fabric-compatible
// specifier string. Descriptor dependencies carry version constraints as
// strings, and the resolver parses them with the module's version dialect.
func renderVersionExpr(expr version.Expr) (string, error) {
	if len(expr) == 0 {
		return "", nil
	}
	alternatives := make([]string, 0, len(expr))
	for _, clause := range expr {
		if len(clause) == 0 {
			return "", nil
		}
		terms := make([]string, 0, len(clause))
		for _, term := range clause {
			if term.Operator == version.OpNever || term.Value == nil {
				return "", fmt.Errorf(
					"Quilt version constraint cannot be represented",
				)
			}
			operator := term.Operator
			if term.Inverted {
				operator = operator.Inverse()
			}
			terms = append(terms, operator.ToSign()+fmt.Sprint(term.Value))
		}
		alternatives = append(alternatives, strings.Join(terms, " "))
	}
	return strings.Join(alternatives, " || "), nil
}

func quiltRole(value any) string {
	switch roles := value.(type) {
	case string:
		return roles
	case []any:
		parts := make([]string, 0, len(roles))
		for _, role := range roles {
			if text, ok := role.(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}
