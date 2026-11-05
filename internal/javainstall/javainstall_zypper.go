package javainstall

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/java"
)

// zypper installs Java from the configured Zypper sources.
type zypper struct{}

func (zypper) method() Method { return MethodZypper }

func (zypper) system() bool { return true }

func (zypper) ownsJava(context.Context, *Host) bool { return false }

func (zypper) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if req.Major <= 0 ||
		req.Distribution != java.DistributionAny ||
		!host.systemToolAvailable("zypper") {
		return nil, nil
	}
	pkg := fmt.Sprintf("java-%d-openjdk-headless", req.Major)
	if !zypperPackageAvailable(ctx, host, pkg) {
		return nil, nil
	}
	return []Plan{
		{
			Method:       MethodZypper,
			Package:      pkg,
			Distribution: java.DistributionAny,
			Major:        req.Major,
			Steps: []Step{
				{
					Args: []string{
						"zypper",
						"--non-interactive",
						"install",
						"--no-recommends",
						pkg,
					},
					Elevation: ElevationRoot,
				},
			},
			KeepDefault: true,
		},
	}, nil
}

// zypperPackageAvailable reports whether the configured sources offer pkg.
func zypperPackageAvailable(
	ctx context.Context,
	host *Host,
	pkg string,
) bool {
	_, err := host.systemProbe(
		ctx,
		"zypper",
		"--non-interactive",
		"--quiet",
		"search",
		"--match-exact",
		"--type",
		"package",
		pkg,
	)
	if err != nil {
		host.logger.Debug("zypper package probe failed", "package", pkg, "reason", err)
		return false
	}
	return true
}
