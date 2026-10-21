package lockfile

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestDecodeInfersMissingModuleDialect(t *testing.T) {
	const oldLock = `lock_version: 1
manifest_fingerprint: sha256:test
server:
  directory: server
  minecraft: 1.21.4
  product: fabric
  core_version: 0.16.9
  provider: modrinth
  project_id: fabric-loader
  version: 1.0.0
  filename: server.jar
  hashes:
    sha256: abc
  bootstrapper: ""
  java: 21
packages:
  - environment: server
    ecosystem: fabric
    provider: modrinth
    project_id: P7dR8mSH
    version: 1.0.0
    filename: mod.jar
    hashes:
      sha256: def
    modules:
      - id: fabric-api
        version: 1.0.0
        ecosystem: fabric
        descriptor_path: fabric.mod.json
`

	document, err := Decode(strings.NewReader(oldLock))
	if err != nil {
		t.Fatalf("decode old lock: %v", err)
	}
	module := document.Packages[0].Modules[0]
	if got := module.EffectiveVersionDialect(); got != types.VersionDialectFabric {
		t.Fatalf("inferred dialect = %q, want %q", got, types.VersionDialectFabric)
	}
	if module.VersionDialect != types.VersionDialectUnknown {
		t.Fatal("decode must not rewrite the optional field")
	}

	document.Packages[0].Modules[0].VersionDialect = types.VersionDialectFabric
	data, err := Snapshot(document)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(string(data), "version_dialect: fabric") {
		t.Fatalf("snapshot omitted version dialect:\n%s", data)
	}
}
