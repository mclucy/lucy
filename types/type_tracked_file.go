package types

// TrackedFile declares accepted contents and a change policy, not an observed file state.
type TrackedFile struct {
	Path       string `yaml:"path" json:"path"`
	Immutable  bool   `yaml:"immutable,omitempty" json:"immutable,omitzero"`
	Hash       string `yaml:"hash,omitempty" json:"hash,omitempty"`
	RecoverURL string `yaml:"recover_url,omitempty" json:"recover_url,omitempty"`
}
