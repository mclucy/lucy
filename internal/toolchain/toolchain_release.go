package toolchain

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/types"
)

// releaseProperties reads the release file of a Java home. A missing or
// unreadable file yields an empty map. Lines are KEY="value"; quotes and
// surrounding blanks are optional, and CRLF is accepted.
func releaseProperties(home string) map[string]string {
	properties := make(map[string]string)
	data, err := os.ReadFile(filepath.Join(home, "release"))
	if err != nil {
		return properties
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"`)
		properties[key] = value
	}
	return properties
}

// distributionFromRelease maps the IMPLEMENTOR value to a distribution. A
// vendor Lucy does not know is an unbranded build for the purpose of matching
// a requirement. Oracle builds need extra keys to tell their three products
// apart.
func distributionFromRelease(properties map[string]string) types.JavaDistribution {
	implementor := properties["IMPLEMENTOR"]
	if distribution, ok := implementorDistributions[implementor]; ok {
		return distribution
	}
	if implementor != "Oracle Corporation" {
		return types.JavaDistributionAny
	}
	switch {
	case properties["GRAALVM_VERSION"] != "":
		return types.JavaDistributionGraalVM
	case properties["BUILD_TYPE"] == "commercial":
		return types.JavaDistributionOracle
	default:
		return types.JavaDistributionOracleOpenJDK
	}
}

// implementorDistributions lists the IMPLEMENTOR values of the vendors Lucy
// installs. Downloadable Oracle GraalVM reports Oracle Corporation and needs
// the GRAALVM_VERSION key, so it is not listed here.
var implementorDistributions = map[string]types.JavaDistribution{
	"Eclipse Adoptium":   types.JavaDistributionTemurin,
	"Azul Systems, Inc.": types.JavaDistributionZulu,
	"Amazon.com Inc.":    types.JavaDistributionCorretto,
	"BellSoft":           types.JavaDistributionLiberica,
	"Microsoft":          types.JavaDistributionMicrosoft,
	"GraalVM Community":  types.JavaDistributionGraalVMCommunity,
	"IBM Corporation":    types.JavaDistributionSemeru,
	"SAP SE":             types.JavaDistributionSapMachine,
	"Alibaba":            types.JavaDistributionDragonwell,
	"Tencent":            types.JavaDistributionKona,
	"Red Hat, Inc.":      types.JavaDistributionRedHat,
	"JetBrains s.r.o.":   types.JavaDistributionJetBrains,
}
