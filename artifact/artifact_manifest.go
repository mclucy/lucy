package artifact

import (
	"archive/zip"
	"bytes"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/version"
)

const manifestPath = "META-INF/MANIFEST.MF"

// readManifestImplementationVersion returns Unknown for a missing manifest or
// attribute. Read failures remain distinguishable for callers that reject them.
func readManifestImplementationVersion(
	zipRdr *zip.Reader,
	maxBytes int64,
) (version.Version, error) {
	raw, found, err := fsutil.ReadZipEntry(zipRdr, manifestPath, maxBytes)
	if err != nil {
		return version.Unknown, err
	}
	if !found {
		return version.Unknown, nil
	}
	return manifestImplementationVersion(raw), nil
}

func manifestImplementationVersion(manifest []byte) version.Version {
	const versionField = "Implementation-Version: "
	for len(manifest) > 0 {
		line := manifest
		if end := bytes.IndexAny(manifest, "\r\n"); end >= 0 {
			line, manifest = manifest[:end], manifest[end+1:]
		} else {
			manifest = nil
		}
		if value, found := bytes.CutPrefix(line, []byte(versionField)); found {
			return version.Version(value)
		}
	}
	return version.Unknown
}
