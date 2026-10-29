package javainstall

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
)

// pacman installs Java from the configured Pacman sources.
type pacman struct{}

func (pacman) method() Method { return MethodPacman }

func (pacman) system() bool { return true }

func (pacman) ownsJava() bool { return false }

func (pacman) plans(ctx context.Context, req Request) ([]Plan, error) {
	if req.Major <= 0 ||
		req.Distribution != java.DistributionAny ||
		!systemToolAvailable("pacman") {
		return nil, nil
	}
	pkg := fmt.Sprintf("jre%d-openjdk-headless", req.Major)
	if !pacmanPackageAvailable(ctx, pkg) {
		info, ok := pacmanPackageInfo(ctx, "jre-openjdk-headless")
		if !ok || !pacmanProvidesMajor(info, req.Major) {
			return nil, nil
		}
		pkg = "jre-openjdk-headless"
	}
	return []Plan{
		{
			Method:       MethodPacman,
			Package:      pkg,
			Distribution: java.DistributionAny,
			Major:        req.Major,
			Steps: []Step{
				{
					Args: []string{
						"pacman",
						"-S",
						"--needed",
						"--noconfirm",
						pkg,
					},
					Elevation: ElevationRoot,
				},
			},
		},
	}, nil
}

// pacmanPackageInfo returns the repository metadata of pkg, or false when the
// configured sources do not offer it.
func pacmanPackageInfo(ctx context.Context, pkg string) (string, bool) {
	output, err := systemProbe(ctx, "pacman", "-Si", pkg)
	if err != nil {
		log.Debug("pacman package probe failed", "package", pkg, "reason", err)
		return "", false
	}
	return string(output), true
}

func pacmanPackageAvailable(ctx context.Context, pkg string) bool {
	_, ok := pacmanPackageInfo(ctx, pkg)
	return ok
}

// pacmanProvidesMajor reports whether the package metadata names major in its
// Provides field, which the unversioned package uses to follow the latest
// release.
func pacmanProvidesMajor(info string, major int) bool {
	want := strconv.Itoa(major)
	inProvides := false
	for line := range strings.Lines(info) {
		trimmed := strings.TrimSpace(line)
		field, value, hasField := strings.Cut(trimmed, ":")
		if hasField {
			field = strings.TrimSpace(field)
		}
		if hasField && !strings.Contains(field, " ") {
			inProvides = field == "Provides"
			if !inProvides {
				continue
			}
		} else if inProvides {
			value = trimmed
		} else {
			continue
		}
		for _, token := range strings.Fields(value) {
			name, version, ok := strings.Cut(token, "=")
			if !ok || version != want {
				continue
			}
			switch name {
			case "java-runtime",
				"java-runtime-headless",
				"jre-openjdk",
				"jre-openjdk-headless":
				return true
			}
		}
	}
	return false
}
