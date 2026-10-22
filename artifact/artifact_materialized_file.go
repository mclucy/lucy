package artifact

// MaterializedFile is a verified artifact file on the local filesystem. Path
// is where the materializer placed it. Hashes records the verified digest
// values for those bytes and must include sha256.
type MaterializedFile struct {
	Path   string
	Hashes map[string]string
}
