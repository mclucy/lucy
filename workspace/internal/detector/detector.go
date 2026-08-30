package detector

// ExecutableDetector is the interface for detecting different types of Minecraft servers.
type ExecutableDetector interface {
	Detect(DetectionContext, *DetectionFile) (*ExecutableEvidence, error)
	Name() string
}
