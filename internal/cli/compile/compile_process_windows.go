package compile

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") {
		arguments := make([]string, 0, len(cmd.Args)-3)
		for _, argument := range cmd.Args[3:] {
			arguments = append(arguments, `"`+strings.ReplaceAll(argument, `"`, `""`)+`"`)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + strings.Join(arguments, " ") + `"`}
	}
	cmd.Cancel = func() error {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
