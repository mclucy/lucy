package detector

// Executable analyzes a file using all registered detectors and collects
// all executable evidence candidates in registration order.
func Executable(context DetectionContext, primary *DetectionFile) *ExecutableCandidates {
	if primary == nil {
		return nil
	}

	candidates := &ExecutableCandidates{
		Candidates: make([]*ExecutableEvidence, 0),
	}
	for _, detector := range getExecutableDetectors() {
		result, err := detector.Detect(context, primary)
		if err != nil || result == nil {
			continue
		}
		candidates.Candidates = append(candidates.Candidates, result)
	}
	return candidates
}
