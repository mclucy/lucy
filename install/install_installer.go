package install

import "github.com/mclucy/lucy/types"

var installers = map[types.PlatformId]platformInstaller{}

func registerInstaller(platform types.PlatformId, installer platformInstaller) {
	if installer == nil {
		panic("install: nil installer")
	}
	installers[platform] = installer
}
