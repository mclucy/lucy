package javainstall

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
)

// dnf installs Java from the configured DNF sources.
type dnf struct{}

func (dnf) method() Method { return MethodDnf }

func (dnf) system() bool { return true }

func (dnf) ownsJava() bool { return false }

func (dnf) plans(ctx context.Context, req Request) ([]Plan, error) {
	if req.Major <= 0 || !systemToolAvailable("dnf") {
		return nil, nil
	}
	if req.Distribution == java.DistributionAny {
		pkg := fmt.Sprintf("java-%d-openjdk-headless", req.Major)
		if !dnfPackageAvailable(ctx, pkg) {
			return nil, nil
		}
		return []Plan{
			dnfPlan(pkg, java.DistributionAny, req.Major, nil),
		}, nil
	}
	packages, ok := dnfBrandedPackages(req.Distribution, req.Major)
	if !ok {
		return nil, nil
	}
	for _, pkg := range packages {
		if dnfPackageAvailable(ctx, pkg) {
			return []Plan{
				dnfPlan(pkg, req.Distribution, req.Major, nil),
			}, nil
		}
	}
	repository := dnfRepository(req.Distribution)
	if repository == nil {
		return nil, nil
	}
	return []Plan{
		dnfPlan(packages[0], req.Distribution, req.Major, repository),
	}, nil
}

func dnfPlan(
	pkg string,
	distribution java.Distribution,
	major int,
	repository *Repository,
) Plan {
	return Plan{
		Method:       MethodDnf,
		Package:      pkg,
		Distribution: distribution,
		Major:        major,
		Repository:   repository,
		Steps: []Step{
			{
				Args:      []string{"dnf", "install", "-y", pkg},
				Elevation: ElevationRoot,
			},
		},
		KeepDefault: true,
	}
}

// dnfPackageAvailable reports whether the configured sources offer pkg. The
// probe never installs or changes repository data.
func dnfPackageAvailable(ctx context.Context, pkg string) bool {
	output, err := systemProbe(ctx, "dnf", "--quiet", "list", "--available", pkg)
	if err != nil {
		log.Debug("dnf package probe failed", "package", pkg, "reason", err)
		return false
	}
	for line := range strings.Lines(string(output)) {
		name, _, _ := strings.Cut(strings.TrimSpace(line), ".")
		if name == pkg {
			return true
		}
	}
	return false
}

// dnfBrandedPackages returns the package names of a vendor build, most
// preferred first, and whether DNF can provide that build.
func dnfBrandedPackages(
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
			fmt.Sprintf("java-%d-amazon-corretto-devel", major),
		}, true
	default:
		return nil, false
	}
}

// dnfRepository returns the vendor DNF source for a build. It returns nil when
// the vendor documents no source for this host.
func dnfRepository(distribution java.Distribution) *Repository {
	switch distribution {
	case java.DistributionTemurin:
		repositoryURL := adoptiumRPMRepository()
		if repositoryURL == "" {
			return nil
		}
		return dnfVendorRepository(
			"Eclipse Adoptium RPM repository",
			repositoryURL,
			"adoptium",
			"3B04D753C9050D9A5D343F39843C48A565F8F04B",
			"https://packages.adoptium.net/artifactory/api/gpg/key/public",
			"Eclipse Adoptium",
		)
	case java.DistributionCorretto:
		return dnfVendorRepository(
			"Amazon Corretto RPM repository",
			"https://yum.corretto.aws/$basearch",
			"corretto",
			"6DC3636DAE534049C8B94623A122542AB04F24E3",
			"https://yum.corretto.aws/corretto.key",
			"Amazon Corretto",
		)
	default:
		return nil
	}
}

// adoptiumRPMRepository returns the Adoptium RPM base URL for this host. The
// vendor publishes a directory per distribution and major version.
func adoptiumRPMRepository() string {
	release := systemRelease()
	switch release["ID"] {
	case "centos", "rhel", "fedora":
	default:
		return ""
	}
	major := systemVersionMajor(release["VERSION_ID"])
	arch := systemRPMArch()
	if major == "" || arch == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://packages.adoptium.net/artifactory/rpm/%s/%s/%s",
		release["ID"],
		major,
		arch,
	)
}

func dnfVendorRepository(
	name string,
	repositoryURL string,
	vendor string,
	fingerprint string,
	keyURL string,
	repositoryName string,
) *Repository {
	keyPath := "/etc/pki/rpm-gpg/" + vendor + ".asc"
	content := fmt.Sprintf(
		"[%s]\nname=%s\nbaseurl=%s\nenabled=1\ngpgcheck=1\ngpgkey=file://%s\n",
		vendor,
		repositoryName,
		repositoryURL,
		keyPath,
	)
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
				Path:    "/etc/yum.repos.d/" + vendor + ".repo",
				Content: content,
			},
		},
		Steps: []Step{
			{
				Args:      []string{"rpm", "--import", keyPath},
				Elevation: ElevationRoot,
			},
		},
	}
}
