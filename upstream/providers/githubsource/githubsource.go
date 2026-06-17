package githubsource

import "github.com/mclucy/lucy/types"

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceGitHub
}
