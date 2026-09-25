package detector

type detectorRegistry struct {
	executableDetectors []ExecutableDetector
}

var registry = &detectorRegistry{
	executableDetectors: make([]ExecutableDetector, 0),
}

func registerExecutableDetector(detector ExecutableDetector) {
	registry.executableDetectors = append(
		registry.executableDetectors,
		detector,
	)
}

func getExecutableDetectors() []ExecutableDetector {
	return registry.executableDetectors
}
