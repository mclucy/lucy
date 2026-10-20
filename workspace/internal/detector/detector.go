package detector

type ExecutableDetector interface {
	Detect(DetectionContext, *DetectionFile) (*RuntimeEvidence, error)
	Name() string
}
