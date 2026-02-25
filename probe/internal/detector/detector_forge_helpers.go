package detector

import (
	"archive/zip"
	"io"
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// getForgeModVersion extracts the version from a Forge JAR's manifest
// when the mod version is set to `${file.jarVersion}`
func getForgeModVersion(zip *zip.Reader) types.RawVersion {
	var r io.ReadCloser
	var err error
	for _, f := range zip.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err = f.Open()
			if err != nil {
				return types.UnknownVersion
			}
			defer tools.CloseReader(r, logger.Warn)
			break
		}
	}

	if r == nil {
		return types.UnknownVersion
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return types.UnknownVersion
	}
	manifest := string(data)
	const versionField = "Implementation-Version: "
	i := strings.Index(manifest, versionField) + len(versionField)
	if i == -1 {
		return types.UnknownVersion
	}
	v := manifest[i:]
	v = strings.Split(v, "\r")[0]
	v = strings.Split(v, "\n")[0]
	return types.RawVersion(v)
}

// parseMavenVersionRange parses Maven version range strings into 2D constraint arrays.
//
// Maven version range format (per Maven POM specification):
// - [1.0,2.0] - inclusive range (>=1.0 AND <=2.0)
// - (1.0,2.0) - exclusive range (>1.0 AND <2.0)
// - [1.0,2.0) - mixed range (>=1.0 AND <2.0)
// - [1.0] - ex≈act version match (hard requirement)
// - 1.0 - soft requirement (or >=1.0 in some contexts)
// - >=1.0, >1.0, <=2.0, <2.0 - single constraint (extension for Forge/NeoForge)
//
// Returns [][]VersionConstraint where:
// - outer array represents OR alternatives
// - inner array represents AND constraints (e.g., range bounds)
//
// Note: This implementation focuses on the most common Maven version range formats
// used by Forge and NeoForge. Full Maven specification supports comma-separated
// multiple ranges like "(,1.0],[1.2,)", but current implementation only processes
// the first range. Operators like >=, >, <=, <, ^, ~ are Forge/NeoForge extensions
// and may not be standard Maven notation.
//
// Version compatibility:
// - Standard Maven syntax: All Maven versions
// - Operator extensions (>=, >, etc.): Primarily used by Forge/NeoForge mods
func parseMavenVersionRange(interval string) [][]types.VersionConstraint {
	interval = strings.TrimSpace(interval)
	if interval == "" || interval == "*" {
		return nil
	}
	if strings.EqualFold(interval, "none") {
		return nil
	}

	interval = strings.ReplaceAll(interval, " ", "")
	// Handle multiple ranges separated by commas (take only first for now)
	for _, pattern := range []struct {
		sep    string
		closer string
	}{
		{sep: "],[", closer: "]"},
		{sep: "),(", closer: ")"},
		{sep: "],(", closer: "]"},
		{sep: "),[", closer: ")"},
	} {
		if strings.Contains(interval, pattern.sep) {
			parts := strings.SplitN(interval, pattern.sep, 2)
			interval = parts[0] + pattern.closer
			break
		}
	}

	if len(interval) >= 2 {
		leftBracket := interval[0]
		rightBracket := interval[len(interval)-1]
		if (leftBracket == '[' || leftBracket == '(') &&
			(rightBracket == ']' || rightBracket == ')') {
			body := interval[1 : len(interval)-1]

			// Range with two bounds: [lower,upper] or (lower,upper)
			if strings.Contains(body, ",") {
				parts := strings.SplitN(body, ",", 2)
				left := strings.TrimSpace(parts[0])
				right := strings.TrimSpace(parts[1])
				var bounds []types.VersionConstraint

				if left != "" {
					op := types.OpGt
					if leftBracket == '[' {
						op = types.OpGte
					}
					bounds = append(
						bounds, types.VersionConstraint{
							Value: dependency.Parse(
								types.RawVersion(left),
								types.Semver,
							),
							Operator: op,
						},
					)
				}
				if right != "" {
					op := types.OpLt
					if rightBracket == ']' {
						op = types.OpLte
					}
					bounds = append(
						bounds, types.VersionConstraint{
							Value: dependency.Parse(
								types.RawVersion(right),
								types.Semver,
							),
							Operator: op,
						},
					)
				}

				// Both bounds are AND constraints, wrap in single inner array
				if len(bounds) > 0 {
					return [][]types.VersionConstraint{bounds}
				}
				return nil
			}

			// Exact version match: [1.0]
			body = strings.TrimSpace(body)
			if body != "" && leftBracket == '[' && rightBracket == ']' {
				return [][]types.VersionConstraint{
					{
						{
							Value: dependency.Parse(
								types.RawVersion(body),
								types.Semver,
							),
							Operator: types.OpEq,
						},
					},
				}
			}
		}
	}

	// Single constraint: >=1.0, >1.0, <=2.0, <2.0, etc.
	version := strings.TrimLeft(interval, "<>=!^~")
	req := types.VersionConstraint{
		Value: dependency.Parse(types.RawVersion(version), types.Semver),
	}
	if strings.HasPrefix(interval, "=") {
		req.Operator = types.OpEq
	} else if strings.HasPrefix(interval, "~") ||
		strings.HasPrefix(interval, "~=") {
		req.Operator = types.OpWeakEq
	} else if strings.HasPrefix(interval, "<=") {
		req.Operator = types.OpLte
	} else if strings.HasPrefix(interval, "<") {
		req.Operator = types.OpLt
	} else if strings.HasPrefix(interval, ">=") {
		req.Operator = types.OpGte
	} else if strings.HasPrefix(interval, ">") {
		req.Operator = types.OpGt
	} else if strings.HasPrefix(interval, "!=") {
		req.Operator = types.OpNeq
	} else if strings.HasPrefix(interval, "^") {
		req.Operator = types.OpWeakGt
	} else {
		req.Operator = types.OpEq
	}

	return [][]types.VersionConstraint{{req}}
}
