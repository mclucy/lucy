package artifact

import (
	"archive/zip"
	"bytes"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/version"
)

const manifestPath = "META-INF/MANIFEST.MF"

// readManifestAttributes parses the main attributes of the archive manifest.
// Attribute names are case-insensitive, so the returned map is keyed by
// lowercased names. An attribute value wraps onto following lines that start
// with one space; the space is removed and the parts join without a separator,
// as the JAR specification defines.
func readManifestAttributes(
	zipRdr *zip.Reader,
	maxBytes int64,
) (map[string]string, error) {
	raw, found, err := fsutil.ReadZipEntry(zipRdr, manifestPath, maxBytes)
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]string{}, nil
	}
	return manifestAttributes(raw), nil
}

func manifestAttributes(manifest []byte) map[string]string {
	attributes := map[string]string{}
	key := ""
	for len(manifest) > 0 {
		line := manifest
		if end := bytes.IndexAny(manifest, "\r\n"); end >= 0 {
			line = manifest[:end]
			manifest = manifest[end+1:]
			if len(line) == 0 {
				// The main section ends at the first blank line.
				break
			}
			if len(manifest) > 0 && manifest[0] == '\n' {
				manifest = manifest[1:]
			}
		} else {
			manifest = nil
		}
		if strings.HasPrefix(string(line), " ") {
			if key != "" {
				attributes[key] += string(line[1:])
			}
			continue
		}
		name, value, found := bytes.Cut(line, []byte(": "))
		if !found {
			name, value, found = bytes.Cut(line, []byte(":"))
			if !found {
				key = ""
				continue
			}
		}
		key = strings.ToLower(string(name))
		attributes[key] = strings.TrimPrefix(string(value), " ")
	}
	return attributes
}

// manifestAttribute returns the value of a main manifest attribute. Names are
// case-insensitive, so the lookup normalizes the requested name.
func manifestAttribute(attributes map[string]string, name string) string {
	return attributes[strings.ToLower(name)]
}

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
