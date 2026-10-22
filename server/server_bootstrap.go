package server

import "github.com/mclucy/lucy/catalog"

type BootstrapInput struct {
	Path             string `yaml:"path" json:"path"`
	catalog.Artifact `yaml:",inline"`
}
