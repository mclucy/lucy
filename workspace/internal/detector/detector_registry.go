package detector

// detectorRegistry manages registered detectors
type detectorRegistry struct {
	executableDetectors []ExecutableDetector
}

// Global registry instance
var registry = &detectorRegistry{
	executableDetectors: make([]ExecutableDetector, 0),
}

// registerExecutableDetector adds a new executable detector to the registry
func registerExecutableDetector(detector ExecutableDetector) {
	registry.executableDetectors = append(
		registry.executableDetectors,
		detector,
	)
}

// getExecutableDetectors returns all registered executable detectors
func getExecutableDetectors() []ExecutableDetector {
	return registry.executableDetectors
}
