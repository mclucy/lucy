package types

import (
	"slices"
	"strings"
)

// JavaDistribution identifies a branded Java build by its foojay name. The
// unspecified distribution accepts any build, including unbranded builds from
// operating system package managers.
type JavaDistribution string

const (
	JavaDistributionAny JavaDistribution = ""

	JavaDistributionTemurin          JavaDistribution = "temurin"
	JavaDistributionZulu             JavaDistribution = "zulu"
	JavaDistributionCorretto         JavaDistribution = "corretto"
	JavaDistributionLiberica         JavaDistribution = "liberica"
	JavaDistributionMicrosoft        JavaDistribution = "microsoft"
	JavaDistributionOracle           JavaDistribution = "oracle"
	JavaDistributionOracleOpenJDK    JavaDistribution = "oracle_open_jdk"
	JavaDistributionGraalVM          JavaDistribution = "graalvm"
	JavaDistributionGraalVMCommunity JavaDistribution = "graalvm_community"
	JavaDistributionSemeru           JavaDistribution = "semeru"
	JavaDistributionSapMachine       JavaDistribution = "sap_machine"
	JavaDistributionDragonwell       JavaDistribution = "dragonwell"
	JavaDistributionKona             JavaDistribution = "kona"
	JavaDistributionRedHat           JavaDistribution = "redhat"
	JavaDistributionJetBrains        JavaDistribution = "jetbrains"
)

// JavaDistributions lists every distribution a requirement may name.
var JavaDistributions = []JavaDistribution{
	JavaDistributionTemurin,
	JavaDistributionZulu,
	JavaDistributionCorretto,
	JavaDistributionLiberica,
	JavaDistributionMicrosoft,
	JavaDistributionOracle,
	JavaDistributionOracleOpenJDK,
	JavaDistributionGraalVM,
	JavaDistributionGraalVMCommunity,
	JavaDistributionSemeru,
	JavaDistributionSapMachine,
	JavaDistributionDragonwell,
	JavaDistributionKona,
	JavaDistributionRedHat,
	JavaDistributionJetBrains,
}

// String returns the persisted key. The unspecified distribution renders as
// "any" for display only.
func (d JavaDistribution) String() string {
	if d == JavaDistributionAny {
		return "any"
	}
	return string(d)
}

// Title returns the human-facing distribution name.
func (d JavaDistribution) Title() string {
	switch d {
	case JavaDistributionAny:
		return "Any"
	case JavaDistributionTemurin:
		return "Eclipse Temurin"
	case JavaDistributionZulu:
		return "Azul Zulu"
	case JavaDistributionCorretto:
		return "Amazon Corretto"
	case JavaDistributionLiberica:
		return "BellSoft Liberica"
	case JavaDistributionMicrosoft:
		return "Microsoft Build of OpenJDK"
	case JavaDistributionOracle:
		return "Oracle JDK"
	case JavaDistributionOracleOpenJDK:
		return "Oracle OpenJDK"
	case JavaDistributionGraalVM:
		return "Oracle GraalVM"
	case JavaDistributionGraalVMCommunity:
		return "GraalVM Community"
	case JavaDistributionSemeru:
		return "IBM Semeru"
	case JavaDistributionSapMachine:
		return "SapMachine"
	case JavaDistributionDragonwell:
		return "Alibaba Dragonwell"
	case JavaDistributionKona:
		return "Tencent Kona"
	case JavaDistributionRedHat:
		return "Red Hat build of OpenJDK"
	case JavaDistributionJetBrains:
		return "JetBrains Runtime"
	default:
		return string(d)
	}
}

// Accepts reports whether a build of distribution actual satisfies d. The
// unspecified distribution accepts every build; a named one accepts only
// itself, so an unbranded build never satisfies it.
func (d JavaDistribution) Accepts(actual JavaDistribution) bool {
	return d == JavaDistributionAny || d == actual
}

// ParseJavaDistribution maps a distribution key to its typed value. Keys are
// case-insensitive, and "any" or the empty key mean the unspecified
// distribution.
func ParseJavaDistribution(s string) (JavaDistribution, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "any" {
		return JavaDistributionAny, true
	}
	if d := JavaDistribution(s); slices.Contains(JavaDistributions, d) {
		return d, true
	}
	return JavaDistributionAny, false
}
