// Package maven converts Maven coordinates to repository paths.
package maven

import (
	"fmt"
	"path"
	"strings"
)

// Path returns the repository path of a coordinate. A coordinate names
// group:artifact:version with an optional classifier, and an optional
// @extension overrides the default jar extension.
func Path(coordinate string) (string, error) {
	base, extension, has := strings.Cut(coordinate, "@")
	if !has {
		extension = "jar"
	}
	parts := strings.Split(base, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("invalid Maven coordinate %q", coordinate)
	}
	name := parts[1] + "-" + parts[2]
	if len(parts) == 4 {
		name += "-" + parts[3]
	}
	return path.Join(
		strings.ReplaceAll(parts[0], ".", "/"),
		parts[1],
		parts[2],
		name+"."+extension,
	), nil
}
