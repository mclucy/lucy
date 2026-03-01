package detector

import (
	"io"
	"os"
	"strings"

	"github.com/mclucy/lucy/types"
)

// analyzeForgeArgFile parses Forge argument files to extract version information
// This is a helper function used by ForgeDetector
func analyzeForgeArgFile(file *os.File) (
	forgeVersion types.RawVersion,
	mcVersion types.RawVersion,
) {
	data, _ := io.ReadAll(file)
	s := string(data)
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "--fml.forgeVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				forgeVersion = types.RawVersion(split[1])
				continue
			}
			forgeVersion = types.UnknownVersion
		}
		if strings.HasPrefix(line, "--fml.mcVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				mcVersion = types.RawVersion(split[1])
				continue
			}
			mcVersion = types.UnknownVersion
		}
	}

	return forgeVersion, mcVersion
}
