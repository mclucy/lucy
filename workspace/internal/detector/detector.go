package detector

type ExecutableDetector interface {
	Detect(DetectionContext, *DetectionFile) (*ExecutableEvidence, error)
	Name() string
}
