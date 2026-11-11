package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fsutil"
)

// ReadModules keeps each descriptor identity and nested container location
// intact. Without a server version it reads the descriptor an inventory reader
// reports; callers that select for a server use ReadModulesFor.
func ReadModules(ctx context.Context, filename string, loader catalog.Ecosystem) ([]catalog.DescriptorModule, error) {
	return ReadModulesFor(ctx, filename, loader, "")
}

// ReadModulesFor keeps each descriptor identity and nested container location
// intact, selecting version-dependent descriptors for minecraft.
func ReadModulesFor(
	ctx context.Context,
	filename string,
	loader catalog.Ecosystem,
	minecraft string,
) ([]catalog.DescriptorModule, error) {
	modules, err := readModulesFor(ctx, filename, loader, minecraft)
	if err != nil {
		return nil, err
	}
	return normalizeModules(modules), nil
}

// ReadClientOnly reports whether every module the loader's descriptor declares
// stays off a dedicated server. known is false when no descriptor for loader
// declares a module, because a jar the loader has no descriptor for is not
// evidence of a client-only package. The traversal matches ReadModulesFor, so a
// client-only descriptor does not contribute its bundled archives.
func ReadClientOnly(
	ctx context.Context,
	filename string,
	loader catalog.Ecosystem,
	minecraft string,
) (clientOnly, known bool, err error) {
	modules, err := readModulesFor(ctx, filename, loader, minecraft)
	if err != nil {
		return false, false, err
	}
	if len(modules) == 0 {
		return false, false, nil
	}
	for _, module := range modules {
		if module.Side != catalog.SideClient {
			return false, true, nil
		}
	}
	return true, true, nil
}

// readModulesFor reads every module the descriptor declares without filtering
// client-only ones.
func readModulesFor(
	ctx context.Context,
	filename string,
	loader catalog.Ecosystem,
	minecraft string,
) ([]catalog.DescriptorModule, error) {
	if strings.EqualFold(filepath.Ext(filename), ".py") {
		return inspectPython(ctx, filename)
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		// Content-addressed storage names an object by its digest, not the
		// artifact's filename. An MCDR plugin source is not an archive, so
		// fall back to its static metadata when the extension is absent.
		if loader == catalog.EcosystemMCDR {
			if modules, pythonErr := inspectPython(ctx, filename); pythonErr == nil {
				return modules, nil
			}
		}
		return nil, err
	}
	defer z.Close()
	return inspectArchive(&z.Reader, loader, "", 0, minecraft)
}

// normalizeModules makes descriptor order independent of map iteration. The
// lock and plan hashes depend on this order.
func normalizeModules(modules []catalog.DescriptorModule) []catalog.DescriptorModule {
	modules = applyClientOnlyIdentities(modules)
	for i := range modules {
		slices.Sort(modules[i].Provides)
		slices.Sort(modules[i].ClientOnly)
		normalizeDescriptorDependencies(modules[i].Dependencies)
	}
	sort.Slice(modules, func(i, j int) bool {
		return compareDescriptorModule(modules[i], modules[j]) < 0
	})
	return modules
}

// applyClientOnlyIdentities drops client-only declarations from the server
// inventory and records their identities on the modules that remain. A loader
// excludes those candidates from a dedicated server, so a requirement whose
// only candidate is client-only is filtered rather than reported missing.
func applyClientOnlyIdentities(
	modules []catalog.DescriptorModule,
) []catalog.DescriptorModule {
	var kept []catalog.DescriptorModule
	var clientOnly []string
	for _, module := range modules {
		if module.Side != catalog.SideClient {
			kept = append(kept, module)
			continue
		}
		clientOnly = append(clientOnly, descriptorModuleNames(module)...)
	}
	if len(clientOnly) == 0 {
		return modules
	}
	if len(kept) == 0 {
		return nil
	}
	for i := range kept {
		kept[i].ClientOnly = mergeModuleNames(kept[i].ClientOnly, clientOnly)
	}
	return kept
}

func descriptorModuleNames(module catalog.DescriptorModule) []string {
	names := make([]string, 0, len(module.Provides)+2)
	if module.ID != "" {
		names = append(names, module.ID)
	}
	names = append(names, module.Provides...)
	if module.Group != "" && module.ID != "" {
		names = append(names, module.Group+":"+module.ID)
	}
	return names
}

func mergeModuleNames(existing, added []string) []string {
	merged := append(append([]string(nil), existing...), added...)
	slices.Sort(merged)
	return slices.Compact(merged)
}

func normalizeDescriptorDependencies(deps []catalog.DescriptorDependency) {
	sort.Slice(deps, func(a, b int) bool {
		return compareDescriptorDependency(deps[a], deps[b]) < 0
	})
	for i := range deps {
		normalizeDescriptorDependencies(deps[i].AnyOf)
		normalizeDescriptorDependencies(deps[i].AllOf)
		normalizeDescriptorDependencies(deps[i].Unless)
	}
}

func compareDescriptorModule(a, b catalog.DescriptorModule) int {
	if n := strings.Compare(a.NestedPath, b.NestedPath); n != 0 {
		return n
	}
	if n := strings.Compare(a.DescriptorPath, b.DescriptorPath); n != 0 {
		return n
	}
	if n := strings.Compare(a.ID, b.ID); n != 0 {
		return n
	}
	if n := strings.Compare(string(a.Version), string(b.Version)); n != 0 {
		return n
	}
	return strings.Compare(string(a.Ecosystem), string(b.Ecosystem))
}

func compareDescriptorDependency(a, b catalog.DescriptorDependency) int {
	if n := strings.Compare(string(a.Kind), string(b.Kind)); n != 0 {
		return n
	}
	if n := strings.Compare(a.ID, b.ID); n != 0 {
		return n
	}
	if n := strings.Compare(a.Constraint, b.Constraint); n != 0 {
		return n
	}
	if n := strings.Compare(a.Ordering, b.Ordering); n != 0 {
		return n
	}
	if n := strings.Compare(a.Phase, b.Phase); n != 0 {
		return n
	}
	if n := strings.Compare(a.Load, b.Load); n != 0 {
		return n
	}
	if a.JoinClasspath != b.JoinClasspath {
		if a.JoinClasspath {
			return 1
		}
		return -1
	}
	if n := strings.Compare(string(a.Provider), string(b.Provider)); n != 0 {
		return n
	}
	if n := strings.Compare(a.ProjectID, b.ProjectID); n != 0 {
		return n
	}
	return strings.Compare(a.ModuleID, b.ModuleID)
}

func nativeEntry(z *zip.Reader, name string) ([]byte, error) {
	raw, _, err := fsutil.ReadZipEntry(
		z,
		name,
		fsutil.MaxNestedZipEntryBytes,
	)
	return raw, err
}

// descriptorDecoders maps each descriptor ecosystem to its decoder.
var descriptorDecoders = map[catalog.Ecosystem]func(
	*zip.Reader,
	string,
	string,
) (*decodedDescriptor, error){
	catalog.EcosystemQuilt: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeQuiltDescriptor(z, member, false)
	},
	catalog.EcosystemFabric: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeFabricDescriptor(z, member, false)
	},
	catalog.EcosystemForge: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeForgeDescriptor(z, member, false)
	},
	catalog.EcosystemNeoForge: func(
		z *zip.Reader,
		member, minecraft string,
	) (*decodedDescriptor, error) {
		return decodeNeoForgeDescriptor(z, member, false, minecraft)
	},
	catalog.EcosystemBukkit: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeBukkitDescriptor(z, member)
	},
	catalog.EcosystemPaper: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeBukkitDescriptor(z, member)
	},
	catalog.EcosystemMCDR: func(
		z *zip.Reader,
		member, _ string,
	) (*decodedDescriptor, error) {
		return decodeMCDRDescriptor(z, member)
	},
}

// inspectArchive reads one archive with the descriptor precedence of loader
// and decodes every nested archive the descriptor bundles.
func inspectArchive(
	z *zip.Reader,
	loader catalog.Ecosystem,
	member string,
	depth int,
	minecraft string,
) ([]catalog.DescriptorModule, error) {
	if depth > 16 {
		return nil, fmt.Errorf("nested archive depth exceeds 16")
	}
	decoded, err := decodeArchiveDescriptor(z, loader, member, minecraft)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return nil, nil
	}

	modules := decoded.catalogModules()
	if decoded.SkipNested {
		return modules, nil
	}
	for _, jar := range decoded.Nested {
		raw, err := nativeEntry(z, jar)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, fmt.Errorf("missing nested JAR %s", jar)
		}
		nested, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, err
		}
		path := jar
		if member != "" {
			path = member + "/" + path
		}
		nestedModules, err := inspectArchive(
			nested,
			loader,
			path,
			depth+1,
			minecraft,
		)
		if err != nil {
			return nil, err
		}
		modules = append(modules, nestedModules...)
	}
	return modules, nil
}

// decodeArchiveDescriptor reads the first descriptor ecosystem in the loader's
// precedence order that the archive carries.
func decodeArchiveDescriptor(
	z *zip.Reader,
	loader catalog.Ecosystem,
	member string,
	minecraft string,
) (*decodedDescriptor, error) {
	for _, ecosystem := range loader.Loads() {
		decoder, ok := descriptorDecoders[ecosystem]
		if !ok {
			continue
		}
		decoded, err := decoder(z, member, minecraft)
		if err != nil {
			return nil, err
		}
		if decoded != nil {
			return decoded, nil
		}
	}
	return nil, nil
}

func inspectPython(
	ctx context.Context,
	path string,
) ([]catalog.DescriptorModule, error) {
	code := `import ast,json,sys
m=ast.parse(open(sys.argv[1],encoding="utf-8").read())
for n in m.body:
 if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=="PLUGIN_METADATA" for t in n.targets):
  print(json.dumps(ast.literal_eval(n.value)));break
else: raise ValueError("missing static PLUGIN_METADATA")`
	data, err := exec.CommandContext(ctx, "python3", "-c", code, path).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Python plugin metadata: %w", err)
	}
	decoded, err := mcdrDecodedDescriptor(nil, data, "")
	if err != nil {
		return nil, err
	}
	return decoded.catalogModules(), nil
}

func ModuleProvides(m catalog.DescriptorModule, id string) bool {
	if m.Ecosystem == catalog.EcosystemBukkit {
		return strings.EqualFold(m.ID, id) || slices.ContainsFunc(m.Provides, func(p string) bool { return strings.EqualFold(p, id) })
	}
	return m.ID == id || slices.Contains(m.Provides, id)
}
