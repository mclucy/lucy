package fileschema

import "encoding/json/v2"

// FileLicense is a descriptor's license field. A loader does not reject a
// descriptor over its license, so decoding accepts what it can and never
// fails: a single name, a list, or an unrecognized shape the caller ignores.
type FileLicense []string

func (l *FileLicense) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*l = []string{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*l = many
	}
	return nil
}
