package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func init() {
	registerInstaller(types.PlatformAny, installGenericPackage)
}

func installGenericPackage(p types.Package) error {
	if p.Remote == nil {
		return errors.New("package remote data is missing")
	}

	result, err := util.CachedDownload(p.Remote.FileUrl, ".", util.DownloadOptions{
		Kind:          cache.KindArtifact,
		Filename:      p.Remote.Filename,
		ExpectedHash:  p.Remote.Hash,
		HashAlgorithm: cache.ParseHashAlgorithm(p.Remote.HashAlgorithm),
	})
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer result.File.Close()

	return nil
}
