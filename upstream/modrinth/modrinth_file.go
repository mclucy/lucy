package modrinth

import (
	"github.com/mclucy/lucy/types"
)

func GetFile(id types.PackageId) (url string, filename string, err error) {
	version, err := getVersion(id)
	if err != nil {
		return "", "", err
	}
	primary := primaryFile(version.Files)
	return primary.Url, primary.Filename, nil
}

func getFile(version *versionResponse) (url string, filename string) {
	primary := primaryFile(version.Files)
	return primary.Url, primary.Filename
}

func primaryFile(files []fileResponse) (primary fileResponse) {
	for _, file := range files {
		if file.Primary {
			return file
		}
	}
	return files[0]
}
