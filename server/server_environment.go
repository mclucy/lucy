package server

// Environment identifies the managed server environment.
type Environment string

const (
	EnvironmentUnknown Environment = ""
	EnvironmentServer  Environment = "server"
	EnvironmentMCDR    Environment = "mcdr"
)
