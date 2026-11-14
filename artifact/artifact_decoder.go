package artifact

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/version"
)

// decodedModule is one descriptor module with both views of one parse. Module
// feeds the resolver, and the remaining fields feed ReadInfo.
type decodedModule struct {
	Module catalog.DescriptorModule
	// PackageEcosystem is the identity ReadInfo reports. Empty means the
	// module ecosystem.
	PackageEcosystem catalog.Ecosystem
	Metadata         catalog.Metadata
	Compatibility    Compatibility
	// PackageVersion overrides the version ReadInfo reports. Empty keeps the
	// module version.
	PackageVersion version.Version
	// PackageDependencies replaces the dependencies ReadInfo derives from
	// Module. A non-nil empty slice means the descriptor declares none.
	PackageDependencies []Dependency
	// Origins maps a module dependency ID to its reader origin.
	Origins map[string]catalog.DependencyOrigin
	// PackageNames maps a module dependency ID to the identity ReadInfo
	// reports.
	PackageNames map[string]string
	// SkipPackage keeps the module in the resolver inventory while ReadInfo
	// omits it.
	SkipPackage bool
}

// decodedDescriptor is one descriptor file together with the archive entries
// that carry further descriptors.
type decodedDescriptor struct {
	Modules []decodedModule
	Nested  []string
	// SkipNested marks a descriptor the loader excludes from the server, which
	// stops its bundled archives from being read.
	SkipNested bool
}

func (d decodedDescriptor) catalogModules() []catalog.DescriptorModule {
	modules := make([]catalog.DescriptorModule, 0, len(d.Modules))
	for _, decoded := range d.Modules {
		modules = append(modules, decoded.Module)
	}
	return modules
}

// packageInfoFromDecoded projects one decoded module into the reader view.
func packageInfoFromDecoded(
	filePath string,
	decoded decodedModule,
) (PackageInfo, error) {
	ecosystem := decoded.PackageEcosystem
	if ecosystem == "" {
		ecosystem = decoded.Module.Ecosystem
	}
	dependencies := decoded.PackageDependencies
	if dependencies == nil {
		var err error
		dependencies, err = packageDependenciesFromModule(decoded, ecosystem)
		if err != nil {
			return PackageInfo{}, err
		}
	}
	packageVersion := decoded.PackageVersion
	if packageVersion == "" {
		packageVersion = decoded.Module.Version
	}
	return PackageInfo{
		Ref: catalog.VersionedRef{
			Ref: catalog.Ref{
				Name:     input.ToProjectName(decoded.Module.ID),
				Provider: catalog.ProviderUnknown,
			},
			Ecosystem: ecosystem,
			Version:   packageVersion,
		},
		FilePath:      filePath,
		Dependencies:  dependencies,
		Metadata:      decoded.Metadata,
		Compatibility: decoded.Compatibility,
	}, nil
}

func packageDependenciesFromModule(
	decoded decodedModule,
	ecosystem catalog.Ecosystem,
) ([]Dependency, error) {
	dependencies := make(
		[]Dependency,
		0,
		len(decoded.Module.Dependencies),
	)
	for _, dependency := range decoded.Module.Dependencies {
		if dependency.Kind == catalog.DependencyKindPython {
			continue
		}
		constraint, err := dependencyExpr(
			dependency,
			decoded.Module.VersionDialect,
		)
		if err != nil {
			return nil, err
		}
		switch dependency.Kind {
		case catalog.DependencyKindIncompatible,
			catalog.DependencyKindConflict:
			constraint = constraint.Negate()
		}

		name := dependency.ID
		if mapped, ok := decoded.PackageNames[dependency.ID]; ok {
			name = mapped
		}
		origin := catalog.DependencyRegular
		if mapped, ok := decoded.Origins[dependency.ID]; ok {
			origin = mapped
		}
		dependencies = append(dependencies, Dependency{
			Ref: catalog.VersionedRef{
				Ref: catalog.Ref{
					Name:     input.ToProjectName(name),
					Provider: catalog.ProviderUnknown,
				},
				Ecosystem: ecosystem,
			},
			Constraint: constraint,
			Mandatory:  dependency.Kind == catalog.DependencyKindRequired,
			Kind:       dependency.Kind,
			Origin:     origin,
		})
	}
	return dependencies, nil
}

// dependencyExpr flattens a descriptor dependency node into the expression the
// reader reports. Any-of alternatives are disjunctive and all-of nodes are
// conjunctive.
func dependencyExpr(
	dependency catalog.DescriptorDependency,
	dialect version.Dialect,
) (version.Expr, error) {
	expr, err := version.ParseConstraint(dependency.Constraint, dialect)
	if err != nil {
		return nil, err
	}
	for _, condition := range dependency.AllOf {
		conjunct, err := dependencyExpr(condition, dialect)
		if err != nil {
			return nil, err
		}
		expr = conjunctionExpr(expr, conjunct)
	}
	for _, alternative := range dependency.AnyOf {
		disjunct, err := dependencyExpr(alternative, dialect)
		if err != nil {
			return nil, err
		}
		if len(disjunct) == 0 {
			return nil, nil
		}
		expr = append(expr, disjunct...)
	}
	return expr, nil
}

// conjunctionExpr multiplies two disjunctive normal form expressions. An empty
// expression imposes no constraint.
func conjunctionExpr(left, right version.Expr) version.Expr {
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

func packageInfosFromDecoded(
	filePath string,
	decoded *decodedDescriptor,
) ([]PackageInfo, error) {
	if decoded == nil {
		return nil, nil
	}
	infos := make([]PackageInfo, 0, len(decoded.Modules))
	for _, module := range decoded.Modules {
		if module.SkipPackage {
			continue
		}
		info, err := packageInfoFromDecoded(filePath, module)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	return infos, nil
}
