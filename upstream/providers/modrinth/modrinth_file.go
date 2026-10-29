package modrinth

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

func GetFile(id catalog.VersionedRef) (
	url string,
	filename string,
	err error,
) {
	version, err := getVersion(upstream.LocalContext{}, id)
	if err != nil {
		return "", "", err
	}
	primary := primaryFile(version.Files)
	return primary.URL, primary.Filename, nil
}

func getFile(version *versionResponse) (url string, filename string) {
	primary := primaryFile(version.Files)
	return primary.URL, primary.Filename
}

func primaryFile(files []fileResponse) (primary fileResponse) {
	for _, file := range files {
		if file.IsPrimary {
			return file
		}
	}
	return files[0]
}
