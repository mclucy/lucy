package install

import (
	"os"
	"os/exec"

	"github.com/mclucy/lucy/workspace"
)

func initMcdr() error {
	err := exec.Command(
		"mcdreforged",
		"--version",
	).Run() // check if mcdreforged is in PATH
	if err != nil {
		return err
	}

	// make subdir
	err = os.Mkdir("server", 0o755)
	if err != nil {
		return err
	}

	// move everything to subdir
	files, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Name() == "server" {
			continue
		}
		err = os.Rename(file.Name(), "server/"+file.Name())
		if err != nil {
			return err
		}
	}

	// init mcdr
	err = exec.Command(
		"mcdreforged",
		"init",
	).Run()
	if err != nil {
		return err
	}

	// rebuild server info
	workspace.Rebuild()

	return nil
}
