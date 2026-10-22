// Package java names the branded Java builds that a server may require. It
// is vocabulary only; runtime selection lives in package javaruntime.
package java

import (
	"slices"
	"strings"
)

// Distribution identifies a branded Java build by its foojay name. The
// unspecified distribution accepts any build, including unbranded builds from
// operating system package managers.
type Distribution string

const (
	DistributionAny Distribution = ""

	DistributionTemurin          Distribution = "temurin"
	DistributionZulu             Distribution = "zulu"
	DistributionCorretto         Distribution = "corretto"
	DistributionLiberica         Distribution = "liberica"
	DistributionMicrosoft        Distribution = "microsoft"
	DistributionOracle           Distribution = "oracle"
	DistributionOracleOpenJDK    Distribution = "oracle_open_jdk"
	DistributionGraalVM          Distribution = "graalvm"
	DistributionGraalVMCommunity Distribution = "graalvm_community"
	DistributionSemeru           Distribution = "semeru"
	DistributionSapMachine       Distribution = "sap_machine"
	DistributionDragonwell       Distribution = "dragonwell"
	DistributionKona             Distribution = "kona"
	DistributionRedHat           Distribution = "redhat"
	DistributionJetBrains        Distribution = "jetbrains"
)

// Distributions lists every distribution a requirement may name.
var Distributions = []Distribution{
	DistributionTemurin,
	DistributionZulu,
	DistributionCorretto,
	DistributionLiberica,
	DistributionMicrosoft,
	DistributionOracle,
	DistributionOracleOpenJDK,
	DistributionGraalVM,
	DistributionGraalVMCommunity,
	DistributionSemeru,
	DistributionSapMachine,
	DistributionDragonwell,
	DistributionKona,
	DistributionRedHat,
	DistributionJetBrains,
}

// String returns the persisted key. The unspecified distribution renders as
// "any" for display only.
func (d Distribution) String() string {
	if d == DistributionAny {
		return "any"
	}
	return string(d)
}

// Title returns the human-facing distribution name.
func (d Distribution) Title() string {
	switch d {
	case DistributionAny:
		return "Any"
	case DistributionTemurin:
		return "Eclipse Temurin"
	case DistributionZulu:
		return "Azul Zulu"
	case DistributionCorretto:
		return "Amazon Corretto"
	case DistributionLiberica:
		return "BellSoft Liberica"
	case DistributionMicrosoft:
		return "Microsoft Build of OpenJDK"
	case DistributionOracle:
		return "Oracle JDK"
	case DistributionOracleOpenJDK:
		return "Oracle OpenJDK"
	case DistributionGraalVM:
		return "Oracle GraalVM"
	case DistributionGraalVMCommunity:
		return "GraalVM Community"
	case DistributionSemeru:
		return "IBM Semeru"
	case DistributionSapMachine:
		return "SapMachine"
	case DistributionDragonwell:
		return "Alibaba Dragonwell"
	case DistributionKona:
		return "Tencent Kona"
	case DistributionRedHat:
		return "Red Hat build of OpenJDK"
	case DistributionJetBrains:
		return "JetBrains Runtime"
	default:
		return string(d)
	}
}

// Accepts reports whether a build of distribution actual satisfies d. The
// unspecified distribution accepts every build; a named one accepts only
// itself, so an unbranded build never satisfies it.
func (d Distribution) Accepts(actual Distribution) bool {
	return d == DistributionAny || d == actual
}

// ParseDistribution maps a distribution key to its typed value. Keys are
// case-insensitive, and "any" or the empty key mean the unspecified
// distribution.
func ParseDistribution(s string) (Distribution, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "any" {
		return DistributionAny, true
	}
	if d := Distribution(s); slices.Contains(Distributions, d) {
		return d, true
	}
	return DistributionAny, false
}
