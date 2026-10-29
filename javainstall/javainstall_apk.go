package javainstall

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/java"
)

// apk installs Java from the configured APK sources.
type apk struct{}

func (apk) method() Method { return MethodApk }

func (apk) system() bool { return true }

func (apk) ownsJava(context.Context, *Host) bool { return false }

func (apk) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if req.Major <= 0 ||
		req.Distribution != java.DistributionAny ||
		!host.systemToolAvailable("apk") {
		return nil, nil
	}
	pkg := fmt.Sprintf("openjdk%d-jre-headless", req.Major)
	if !apkPackageAvailable(ctx, host, pkg) {
		return nil, nil
	}
	return []Plan{
		{
			Method:       MethodApk,
			Package:      pkg,
			Distribution: java.DistributionAny,
			Major:        req.Major,
			Steps: []Step{
				{
					Args: []string{
						"apk",
						"add",
						"--no-cache",
						pkg,
					},
					Elevation: ElevationRoot,
				},
			},
		},
	}, nil
}

// apkPackageAvailable reports whether the configured sources offer pkg. The
// probe never changes the package index.
func apkPackageAvailable(
	ctx context.Context,
	host *Host,
	pkg string,
) bool {
	output, err := host.systemProbe(ctx, "apk", "search", "--exact", pkg)
	if err != nil {
		host.logger.Debug("apk package probe failed", "package", pkg, "reason", err)
		return false
	}
	for line := range strings.Lines(string(output)) {
		name := strings.TrimSpace(line)
		if name == pkg || strings.HasPrefix(name, pkg+"-") {
			return true
		}
	}
	return false
}
