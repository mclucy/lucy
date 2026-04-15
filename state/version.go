package state

const SupportedVersion = "v1"

func ValidateVersion(version string) error {
	if version == "" {
		return NewStateError("", ErrMalformed, "version", "version is required")
	}
	if version != SupportedVersion {
		return NewStateError("", ErrVersionUnsupported, "version", "unsupported version \""+version+"\"; supported version is \""+SupportedVersion+"\"")
	}
	return nil
}

func IsSupported(version string) bool {
	return version == SupportedVersion
}
