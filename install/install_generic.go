package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func init() {
	registerInstaller(types.AnyPlatform, installGenericPackage)
}

func installGenericPackage(p types.Package) error {
	if p.Remote == nil {
		return errors.New("package remote data is missing")
	}

	_, _, err := util.DownloadFile(p.Remote.FileUrl, ".")
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}
