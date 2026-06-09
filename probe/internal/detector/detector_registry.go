package detector

// detectorRegistry manages registered detectors
type detectorRegistry struct {
	executableDetectors   []ExecutableDetector
	jarPackageDetectors   []PackageDetector
	otherPackageDetectors map[string]PackageDetector
}

// Global registry instance
var registry = &detectorRegistry{
	executableDetectors:   make([]ExecutableDetector, 0),
	jarPackageDetectors:   make([]PackageDetector, 0),
	otherPackageDetectors: make(map[string]PackageDetector),
}

// registerExecutableDetector adds a new executable detector to the registry
func registerExecutableDetector(detector ExecutableDetector) {
	registry.executableDetectors = append(
		registry.executableDetectors,
		detector,
	)
}

// registerModDetector adds a new mod detector to the registry
func registerModDetector(detector PackageDetector) {
	registry.jarPackageDetectors = append(
		registry.jarPackageDetectors,
		detector,
	)
}

func registerOtherPackageDetector(detector PackageDetector) {
	registry.otherPackageDetectors[detector.Name()] = detector
}

// getExecutableDetectors returns all registered executable detectors
func getExecutableDetectors() []ExecutableDetector {
	return registry.executableDetectors
}

// getModDetectors returns all registered mod detectors
func getModDetectors() []PackageDetector {
	return registry.jarPackageDetectors
}

func getOtherPackageDetectors() map[string]PackageDetector {
	return registry.otherPackageDetectors
}
