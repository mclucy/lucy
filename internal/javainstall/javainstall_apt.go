package javainstall

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/java"
)

// apt installs Java from the configured APT sources.
type apt struct{}

func (apt) method() Method { return MethodApt }

func (apt) system() bool { return true }

func (apt) ownsJava(context.Context, *Host) bool { return false }

func (apt) plans(ctx context.Context, req Request, host *Host) ([]Plan, error) {
	if req.Major <= 0 || !host.systemToolAvailable("apt-get", "apt-cache") {
		return nil, nil
	}
	if req.Distribution == java.DistributionAny {
		pkg := fmt.Sprintf("openjdk-%d-jre-headless", req.Major)
		if !aptPackageAvailable(ctx, host, pkg) {
			return nil, nil
		}
		return []Plan{
			aptPlan(pkg, java.DistributionAny, req.Major, nil),
		}, nil
	}
	packages, ok := aptBrandedPackages(req.Distribution, req.Major)
	if !ok {
		return nil, nil
	}
	for _, pkg := range packages {
		if aptPackageAvailable(ctx, host, pkg) {
			return []Plan{
				aptPlan(pkg, req.Distribution, req.Major, nil),
			}, nil
		}
	}
	repository := aptRepository(req.Distribution, host)
	if repository == nil {
		return nil, nil
	}
	return []Plan{
		aptPlan(packages[0], req.Distribution, req.Major, repository),
	}, nil
}

func aptPlan(
	pkg string,
	distribution java.Distribution,
	major int,
	repository *Repository,
) Plan {
	return Plan{
		Method:       MethodApt,
		Package:      pkg,
		Distribution: distribution,
		Major:        major,
		Repository:   repository,
		Steps: []Step{
			{
				Args: []string{
					"env",
					"DEBIAN_FRONTEND=noninteractive",
					"apt-get",
					"install",
					"-y",
					"--no-install-recommends",
					pkg,
				},
				Elevation: ElevationRoot,
			},
		},
		KeepDefault: true,
	}
}

// aptPackageAvailable reports whether the configured sources offer pkg. The
// probe never changes the package index.
func aptPackageAvailable(
	ctx context.Context,
	host *Host,
	pkg string,
) bool {
	output, err := host.systemProbe(ctx, "apt-cache", "policy", pkg)
	if err != nil {
		host.logger.Debug("apt package probe failed", "package", pkg, "reason", err)
		return false
	}
	for line := range strings.Lines(string(output)) {
		candidate, ok := strings.CutPrefix(
			strings.TrimSpace(line),
			"Candidate:",
		)
		if !ok {
			continue
		}
		candidate = strings.TrimSpace(candidate)
		return candidate != "" && candidate != "(none)"
	}
	return false
}

// aptBrandedPackages returns the package names of a vendor build, most
// preferred first, and whether APT can provide that build.
func aptBrandedPackages(
	distribution java.Distribution,
	major int,
) ([]string, bool) {
	switch distribution {
	case java.DistributionTemurin:
		return []string{
			fmt.Sprintf("temurin-%d-jre", major),
			fmt.Sprintf("temurin-%d-jdk", major),
		}, true
	case java.DistributionZulu:
		return []string{
			fmt.Sprintf("zulu%d-jre-headless", major),
			fmt.Sprintf("zulu%d-jdk-headless", major),
		}, true
	case java.DistributionCorretto:
		return []string{
			fmt.Sprintf("java-%d-amazon-corretto-jdk", major),
		}, true
	default:
		return nil, false
	}
}

// aptRepository returns the vendor APT source for a build. It returns nil
// when the vendor documents no source for this host.
func aptRepository(distribution java.Distribution, host *Host) *Repository {
	switch distribution {
	case java.DistributionTemurin:
		release := host.systemRelease()
		suite := release["VERSION_CODENAME"]
		if suite == "" {
			suite = release["UBUNTU_CODENAME"]
		}
		if suite == "" {
			return nil
		}
		return aptVendorRepository(
			"Eclipse Adoptium apt repository",
			"https://packages.adoptium.net/artifactory/deb",
			"adoptium",
			"3B04D753C9050D9A5D343F39843C48A565F8F04B",
			"https://packages.adoptium.net/artifactory/api/gpg/key/public",
			suite,
		)
	case java.DistributionZulu:
		return aptVendorRepository(
			"Azul Zulu apt repository",
			"https://repos.azul.com/zulu/deb",
			"azul",
			"27BC0C8CB3D81623F59BDADCB1998361219BD9C9",
			"https://repos.azul.com/azul-repo.key",
			"stable",
		)
	case java.DistributionCorretto:
		return aptVendorRepository(
			"Amazon Corretto apt repository",
			"https://apt.corretto.aws",
			"corretto",
			"6DC3636DAE534049C8B94623A122542AB04F24E3",
			"https://apt.corretto.aws/corretto.key",
			"stable",
		)
	default:
		return nil
	}
}

func aptVendorRepository(
	name string,
	repositoryURL string,
	vendor string,
	fingerprint string,
	keyURL string,
	suite string,
) *Repository {
	keyPath := "/etc/apt/keyrings/" + vendor + ".asc"
	return &Repository{
		Name: name,
		URL:  repositoryURL,
		Key: &RepositoryKey{
			URL:         keyURL,
			Fingerprint: fingerprint,
			Path:        keyPath,
			Armored:     true,
		},
		Files: []RepositoryFile{
			{
				Path: "/etc/apt/sources.list.d/" + vendor + ".list",
				Content: fmt.Sprintf(
					"deb [signed-by=%s] %s %s main\n",
					keyPath,
					repositoryURL,
					suite,
				),
			},
		},
		Steps: []Step{
			{
				Args:      []string{"apt-get", "update"},
				Elevation: ElevationRoot,
			},
		},
	}
}
