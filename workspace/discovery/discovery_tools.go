package discovery

import (
	"archive/zip"
	"encoding/json/v2"
	"os"
	"strings"

	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/version"
)

// analyzeForgeArgFile extracts the Forge and Minecraft versions from a
// Forge argument file.
func analyzeForgeArgFile(file *os.File) (
	forgeVersion version.Version,
	mcVersion version.Version,
) {
	data, _ := fsutil.CopyBytes(file, fsutil.MaxZipEntryBytes)
	s := string(data)
	lines := strings.SplitSeq(s, "\n")
	for line := range lines {
		if strings.HasPrefix(line, "--fml.forgeVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				forgeVersion = version.Version(split[1])
				continue
			}
			forgeVersion = version.Unknown
		}
		if strings.HasPrefix(line, "--fml.mcVersion") {
			split := strings.Split(line, " ")
			if len(split) == 2 {
				mcVersion = version.Version(split[1])
				continue
			}
			mcVersion = version.Unknown
		}
	}

	return forgeVersion, mcVersion
}

// mojangVersionJSONEntry is the version spec entry inside a Mojang server jar.
const mojangVersionJSONEntry = "version.json"

// mojangVersionFromJSON extracts the game version from a Mojang version.json
// payload. guarded reports a Forge installer jar, which also carries a
// version.json but with _comment and/or mainClass control fields a real spec
// never has. encoding/json ignores unknown fields, so the guard fields must
// be checked explicitly.
func mojangVersionFromJSON(data []byte) (version.Version, bool, error) {
	guard := &struct {
		Comment   []string `json:"_comment"`
		MainClass string   `json:"mainClass"`
	}{}
	if err := json.Unmarshal(data, guard); err == nil &&
		(len(guard.Comment) > 0 || guard.MainClass != "") {
		return version.Unknown, true, nil
	}

	var spec fileschema.FileMinecraftVersionSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return version.Unknown, false, err
	}
	return version.Version(spec.ID), false, nil
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
