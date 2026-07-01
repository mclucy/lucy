package detector

import (
	"archive/zip"
	"os"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/types"
)

// analyzeForgeArgFile parses Forge argument files to extract version information
// This is a helper function used by ForgeDetector
func analyzeForgeArgFile(file *os.File) (
	forgeVersion types.BareVersion,
	mcVersion types.BareVersion,
) {
	data, _ := fsutil.CopyBytes(file, fsutil.MaxZipEntryBytes)
	s := string(data)
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "--fml.forgeVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				forgeVersion = types.BareVersion(split[1])
				continue
			}
			forgeVersion = types.VersionUnknown
		}
		if strings.HasPrefix(line, "--fml.mcVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				mcVersion = types.BareVersion(split[1])
				continue
			}
			mcVersion = types.VersionUnknown
		}
	}

	return forgeVersion, mcVersion
}

func readArchiveEntry(zipReader *zip.Reader, name string) ([]byte, bool, error) {
	for _, file := range zipReader.File {
		if file.Name != name {
			continue
		}

		r, err := file.Open()
		if err != nil {
			return nil, false, err
		}
		defer r.Close()

		data, err := fsutil.CopyBytes(r, fsutil.MaxZipEntryBytes)
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}

	return nil, false, nil
}
