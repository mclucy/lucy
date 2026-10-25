package manifest

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/java"
	"gopkg.in/yaml.v3"
)

// JavaAuto is the value that keeps a Java field on auto. Lucy never writes it:
// an absent field means the same.
const JavaAuto = "auto"

// MinimumJava is the oldest Java major version that any Minecraft server
// release runs on.
const MinimumJava = 8

// Java overrides the Java runtime that the server runs on. A nil Java or a zero
// field keeps that part on auto: the version follows the Mojang advisory for
// the Minecraft version, and every distribution is accepted.
type Java struct {
	Version      int               `yaml:"version,omitempty" json:"version,omitempty"`
	Distribution java.Distribution `yaml:"distribution,omitempty" json:"distribution,omitempty"`
}

// IsAuto reports whether j leaves both the version and the distribution on
// auto.
func (j *Java) IsAuto() bool {
	return j == nil || *j == Java{}
}

// UnmarshalYAML accepts a major version or "auto" as a scalar, or a mapping
// with version and distribution keys.
func (j *Java) UnmarshalYAML(n *yaml.Node) error {
	*j = Java{}
	switch n.Kind {
	case yaml.ScalarNode:
		version, err := parseJavaVersion(n.Value)
		if err != nil {
			return fmt.Errorf("server.java: %w", err)
		}
		j.Version = version
		return nil
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			key, value := n.Content[i].Value, n.Content[i+1]
			if value.Kind != yaml.ScalarNode {
				return fmt.Errorf("server.java.%s must be a scalar", key)
			}
			switch key {
			case "version":
				version, err := parseJavaVersion(value.Value)
				if err != nil {
					return fmt.Errorf("server.java.version: %w", err)
				}
				j.Version = version
			case "distribution":
				distribution, err := ParseJavaDistribution(value.Value)
				if err != nil {
					return fmt.Errorf("server.java.distribution: %w", err)
				}
				j.Distribution = distribution
			default:
				return fmt.Errorf("unknown server.java field %q", key)
			}
		}
		return nil
	default:
		return fmt.Errorf("server.java must be a Java major version, %s, or an object", JavaAuto)
	}
}

// MarshalYAML writes the short form when only the version is set.
func (j Java) MarshalYAML() (any, error) {
	if j.Distribution == java.DistributionAny {
		return j.Version, nil
	}
	type plain Java
	return plain(j), nil
}

// ParseJavaDistribution reads a distribution key, where "auto" and "any" mean
// every distribution.
func ParseJavaDistribution(s string) (java.Distribution, error) {
	if strings.EqualFold(strings.TrimSpace(s), JavaAuto) {
		return java.DistributionAny, nil
	}
	distribution, ok := java.ParseDistribution(s)
	if !ok {
		names := make([]string, len(java.Distributions))
		for i, known := range java.Distributions {
			names[i] = string(known)
		}
		return java.DistributionAny, fmt.Errorf(
			"unknown Java distribution %q; use one of %s",
			s,
			strings.Join(names, ", "),
		)
	}
	return distribution, nil
}

func parseJavaVersion(s string) (int, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, JavaAuto) {
		return 0, nil
	}
	version, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a Java major version or %s", s, JavaAuto)
	}
	if version < MinimumJava {
		return 0, fmt.Errorf("major version must be %d or later, got %d", MinimumJava, version)
	}
	return version, nil
}

// normalizeJava drops an override that leaves everything on auto, so the
// manifest and its fingerprint stay the same as without the field.
func normalizeJava(s *Server) error {
	if s.Java.IsAuto() {
		s.Java = nil
		return nil
	}
	if s.Java.Version != 0 && s.Java.Version < MinimumJava {
		return fmt.Errorf("server.java.version must be %d or later", MinimumJava)
	}
	distribution, ok := java.ParseDistribution(string(s.Java.Distribution))
	if !ok {
		return fmt.Errorf("unknown Java distribution %q", s.Java.Distribution)
	}
	s.Java.Distribution = distribution
	return nil
}
