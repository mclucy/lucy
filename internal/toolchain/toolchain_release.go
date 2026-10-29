package toolchain

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/java"
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
func distributionFromRelease(properties map[string]string) java.Distribution {
	implementor := properties["IMPLEMENTOR"]
	if distribution, ok := implementorDistributions[implementor]; ok {
		return distribution
	}
	if implementor != "Oracle Corporation" {
		return java.DistributionAny
	}
	switch {
	case properties["GRAALVM_VERSION"] != "":
		return java.DistributionGraalVM
	case properties["BUILD_TYPE"] == "commercial":
		return java.DistributionOracle
	default:
		return java.DistributionOracleOpenJDK
	}
}

// implementorDistributions lists the IMPLEMENTOR values of the vendors Lucy
// installs. Downloadable Oracle GraalVM reports Oracle Corporation and needs
// the GRAALVM_VERSION key, so it is not listed here.
var implementorDistributions = map[string]java.Distribution{
	"Eclipse Adoptium":   java.DistributionTemurin,
	"Azul Systems, Inc.": java.DistributionZulu,
	"Amazon.com Inc.":    java.DistributionCorretto,
	"BellSoft":           java.DistributionLiberica,
	"Microsoft":          java.DistributionMicrosoft,
	"GraalVM Community":  java.DistributionGraalVMCommunity,
	"IBM Corporation":    java.DistributionSemeru,
	"SAP SE":             java.DistributionSapMachine,
	"Alibaba":            java.DistributionDragonwell,
	"Tencent":            java.DistributionKona,
	"Red Hat, Inc.":      java.DistributionRedHat,
	"JetBrains s.r.o.":   java.DistributionJetBrains,
}
