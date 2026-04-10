package slugresolve

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/curseforge"
	"github.com/mclucy/lucy/upstream/modrinth"
)

func ResolveSlug(
	src types.Source,
	localId string,
	filePath string,
	metadataURLs []string,
) string {
	var fileHash string
	if filePath != "" {
		fileHash = sha256File(filePath)
	}

	if fileHash != "" {
		if slug, ok := slugmap.Default().Get(src, localId, fileHash); ok {
			return slug
		}
	}

	hintSlug := ""
	if slug, ok := slugmap.Default().GetLoose(src, localId); ok {
		hintSlug = slug
	}

	if hintSlug == "" {
		for _, u := range metadataURLs {
			urlSrc, s, ok := ExtractFromURL(u)
			if ok && urlSrc == src && s != "" {
				hintSlug = s
				break
			}
		}
	}

	if filePath != "" {
		var slug string
		var err error
		switch src {
		case types.SourceModrinth:
			slug, err = modrinth.SlugFromFilePathWithHint(filePath, hintSlug)
		case types.SourceCurseForge:
			slug, err = curseforge.SlugFromFilePathWithHint(filePath, hintSlug)
		}
		if err == nil && slug != "" {
			if fileHash != "" {
				slugmap.Default().Set(src, localId, fileHash, slug, "hash")
			}
			return slug
		}
	}

	return localId
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}

	return hex.EncodeToString(h.Sum(nil))
}
