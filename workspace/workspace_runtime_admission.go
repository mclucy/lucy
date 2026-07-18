package workspace

import "github.com/mclucy/lucy/types"

type AdmissionVerdict uint8

const (
	AdmissionUnresolved AdmissionVerdict = iota
	AdmissionDirect
	AdmissionDegraded
	AdmissionRejected
)

type RuntimeAdmission struct {
	Verdict  AdmissionVerdict
	Required types.Ecosystem
	Offered  types.Ecosystem
}

func EvaluateAdmission(
	server *ServerInstance,
	required types.Ecosystem,
) RuntimeAdmission {
	admission := RuntimeAdmission{
		Verdict:  AdmissionRejected,
		Required: required,
	}
	if server == nil || !server.IsValid() {
		admission.Verdict = AdmissionUnresolved
		return admission
	}
	if required == types.EcoUnspecified {
		admission.Verdict = AdmissionDirect
		return admission
	}

	offers := server.EffectiveEcosystems()
	for _, offer := range offers {
		if offer.Verdict != types.CompatCompatible ||
			!offer.Ecosystem.Satisfy(required) {
			continue
		}
		admission.Verdict = AdmissionDirect
		admission.Offered = offer.Ecosystem
		return admission
	}
	for _, offer := range offers {
		if offer.Verdict != types.CompatDegraded ||
			!offer.Ecosystem.Satisfy(required) {
			continue
		}
		admission.Verdict = AdmissionDegraded
		admission.Offered = offer.Ecosystem
		return admission
	}
	return admission
}
