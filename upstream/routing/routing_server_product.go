package routing

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// ServerProductRequest describes a server product resolution request.
type ServerProductRequest struct {
	Minecraft      version.Version
	Product        server.Product
	ProductVersion version.Version
	Repository     string
	FilePattern    string
}

// ServerProductResolution records the artifact selected for a server product
// and the bootstrap inputs needed to run it.
type ServerProductResolution struct {
	Minecraft      version.Version
	Product        server.Product
	ProductVersion version.Version
	Artifact       catalog.Artifact
	Bootstrapper   string
	Inputs         []server.BootstrapInput
	// JavaAdvisory is the Java major version Mojang's launcher ships for the
	// Minecraft version. Lucy treats it as an advisory minimum.
	JavaAdvisory int
}
