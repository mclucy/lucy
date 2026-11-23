package artifact

import (
	"context"

	"github.com/mclucy/lucy/catalog"
)

// MaterializedFile is a verified artifact file on the local filesystem. Path
// is where the materializer placed it. Hashes records the verified digest
// values for those bytes and must include sha256.
type MaterializedFile struct {
	Path   string
	Hashes map[string]string
}

// Materializer acquires verified local bytes for one artifact. It must leave
// its input unchanged. Cancelling ctx abandons the acquisition, so a caller
// that stops needing the bytes does not wait for a transfer it started.
type Materializer func(ctx context.Context, a *catalog.Artifact) (MaterializedFile, error)
