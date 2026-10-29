package javainstall

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"slices"

	"github.com/mclucy/lucy/java"
)

// winget is the Windows Package Manager.
type winget struct{}

// wingetJavaPackage describes the package identifiers of one distribution.
// The formats carry one %d directive for the major. An empty format marks a
// build that winget does not package.
type wingetJavaPackage struct {
	distribution java.Distribution
	jreFormat    string
	jdkFormat    string
	jreMajors    []int
	jdkMajors    []int
}

// wingetJavaPackages lists the package identifiers that exist for each
// distribution.
var wingetJavaPackages = []wingetJavaPackage{
	{
		distribution: java.DistributionTemurin,
		jreFormat:    "EclipseAdoptium.Temurin.%d.JRE",
		jdkFormat:    "EclipseAdoptium.Temurin.%d.JDK",
		jreMajors:    []int{8, 11, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
		jdkMajors:    []int{8, 11, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26},
	},
	{
		distribution: java.DistributionZulu,
		jreFormat:    "Azul.Zulu.%d.JRE",
		jdkFormat:    "Azul.Zulu.%d.JDK",
		jreMajors:    []int{7, 8, 9, 11, 12, 13, 15, 16, 17, 18, 21, 25},
		jdkMajors: []int{
			6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22,
			23, 24, 25, 26, 27,
		},
	},
	{
		distribution: java.DistributionCorretto,
		jreFormat:    "Amazon.Corretto.%d.JRE",
		jdkFormat:    "Amazon.Corretto.%d.JDK",
		jreMajors:    []int{8},
		jdkMajors: []int{
			8, 11, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27,
		},
	},
	{
		distribution: java.DistributionLiberica,
		jreFormat:    "BellSoft.LibericaJRE.%d",
		jdkFormat:    "BellSoft.LibericaJDK.%d",
		jreMajors: []int{
			8, 11, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26,
		},
		jdkMajors: []int{
			8, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25,
			26,
		},
	},
	{
		distribution: java.DistributionMicrosoft,
		jdkFormat:    "Microsoft.OpenJDK.%d",
		jdkMajors:    []int{11, 16, 17, 21, 25},
	},
	{
		distribution: java.DistributionOracle,
		jdkFormat:    "Oracle.JDK.%d",
		jdkMajors: []int{
			17, 18, 19, 20, 21, 22, 23, 24, 25, 26,
		},
	},
	{
		distribution: java.DistributionSapMachine,
		jreFormat:    "SAP.SapMachine.%d.JRE",
		jdkFormat:    "SAP.SapMachine.%d.JDK",
		jreMajors:    []int{11, 17, 21, 22, 23, 24, 25, 26, 27},
		jdkMajors:    []int{11, 17, 21, 22, 23, 24, 25, 26, 27},
	},
	{
		distribution: java.DistributionSemeru,
		jreFormat:    "IBM.Semeru.%d.JRE",
		jdkFormat:    "IBM.Semeru.%d.JDK",
		jreMajors: []int{
			8, 11, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27,
		},
		jdkMajors: []int{
			8, 11, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27,
		},
	},
}

func (winget) method() Method { return MethodWinget }

func (winget) plans(ctx context.Context, req Request) ([]Plan, error) {
	if runtime.GOOS != "windows" {
		return nil, nil
	}
	if _, err := exec.LookPath("winget.exe"); err != nil {
		return nil, nil
	}
	return wingetPlans(req), nil
}

func (winget) system() bool   { return true }
func (winget) ownsJava() bool { return false }

// wingetPlans returns the plan that winget installs req with. At most one
// plan results. A request for any build prefers the Temurin JRE.
func wingetPlans(req Request) []Plan {
	identifier, distribution, ok := wingetJavaIdentifier(req)
	if !ok {
		return nil
	}
	return []Plan{{
		Method:       MethodWinget,
		Package:      identifier,
		Distribution: distribution,
		Major:        req.Major,
		Steps: []Step{{
			Args: []string{
				"winget",
				"install",
				"--exact",
				"--id",
				identifier,
				"--source",
				"winget",
			},
			Elevation: ElevationSelf,
		}},
	}}
}

// wingetJavaIdentifier returns the package identifier for req, the build it
// installs, and whether winget packages req. The JRE package is preferred to
// the JDK package.
func wingetJavaIdentifier(req Request) (string, java.Distribution, bool) {
	distribution := req.Distribution
	if distribution == java.DistributionAny {
		distribution = java.DistributionTemurin
	}
	for _, pkg := range wingetJavaPackages {
		if pkg.distribution != distribution {
			continue
		}
		if pkg.jreFormat != "" && slices.Contains(pkg.jreMajors, req.Major) {
			return fmt.Sprintf(pkg.jreFormat, req.Major), pkg.distribution, true
		}
		if pkg.jdkFormat != "" && slices.Contains(pkg.jdkMajors, req.Major) {
			return fmt.Sprintf(pkg.jdkFormat, req.Major), pkg.distribution, true
		}
	}
	return "", java.DistributionAny, false
}
