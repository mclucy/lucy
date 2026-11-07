package version

import (
	"encoding/json/jsontext"
	"strings"
)

// StringVersion is a version outside the semantic grammar. Fabric Loader
// compares it as a plain string; the opaque dialect uses it for identifiers.
type StringVersion struct {
	dialect  Dialect
	original string
}

func (v *StringVersion) Dialect() Dialect {
	if v == nil {
		return DialectUnknown
	}
	return v.dialect
}

func (v *StringVersion) Compare(other ResolvableVersion) (int, bool) {
	if v == nil || other == nil || other.Dialect() != v.dialect {
		return 0, false
	}
	switch other := other.(type) {
	case *StringVersion:
		if v.dialect == DialectOpaque {
			if v.original != other.original {
				return 0, false
			}
			return 0, true
		}
		return strings.Compare(v.original, other.original), true
	case *SemverVersion:
		return strings.Compare(v.original, other.String()), true
	default:
		return 0, false
	}
}

func (v *StringVersion) Validate() bool {
	return v != nil && v.original != ""
}

func (v *StringVersion) String() string {
	if v == nil {
		return ""
	}
	return v.original
}

// MarshalJSONTo renders the version verbatim for the same reason as
// SemverVersion.
func (v *StringVersion) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(v.String()))
}
