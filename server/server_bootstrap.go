package server

import "fmt"

// BootstrapMethod identifies the algorithm that prepares a server tree from
// its locked core artifact and inputs. The values are persisted in
// lucy-lock.yaml.
type BootstrapMethod string

const (
	// BootstrapMethodVanilla copies the core artifact as server.jar.
	BootstrapMethodVanilla BootstrapMethod = "vanilla"
	// BootstrapMethodPaperclip copies the core artifact and runs Paperclip in
	// patch-only mode.
	BootstrapMethodPaperclip BootstrapMethod = "paperclip"
	// BootstrapMethodFabric places the loader in the library layout and
	// generates launch files.
	BootstrapMethodFabric BootstrapMethod = "fabric"
	// BootstrapMethodQuilt places the Quilt loader in the library layout and
	// generates the Quilt launcher JAR and its properties.
	BootstrapMethodQuilt BootstrapMethod = "quilt"
	// BootstrapMethodForge runs the processors of a pinned Forge or NeoForge
	// installer profile. Both products share the installer profile format.
	BootstrapMethodForge BootstrapMethod = "forge"
)

// Validate rejects a method that no build of Lucy implements.
func (m BootstrapMethod) Validate() error {
	switch m {
	case BootstrapMethodVanilla,
		BootstrapMethodPaperclip,
		BootstrapMethodFabric,
		BootstrapMethodQuilt,
		BootstrapMethodForge:
		return nil
	case "":
		return fmt.Errorf("bootstrap method is required; resolve the server again")
	default:
		return fmt.Errorf("unsupported bootstrap method %q; resolve the server again", m)
	}
}
