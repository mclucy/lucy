package server

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mclucy/lucy/state"
)

type ServiceState struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	NativeID  string `json:"native_id"`
	Manager   string `json:"manager"`
	Detail    string `json:"detail,omitempty"`
}

type ServiceManager interface {
	InstallDaemon(binary string) error
	InstallInstance(inst Instance, binary string) error
	RemoveInstance(inst Instance) error
	EnableDaemon() error
	StartDaemon() error
	EnableInstance(inst Instance) error
	DisableInstance(inst Instance) error
	StartInstance(inst Instance) error
	StopInstance(inst Instance) error
	RestartInstance(inst Instance) error
	StatusDaemon() ServiceState
	StatusInstance(inst Instance) ServiceState
}

func NewServiceManager() ServiceManager {
	switch runtime.GOOS {
	case "darwin":
		return launchdManager{}
	default:
		return systemdManager{}
	}
}

func CurrentBinary() string {
	path, err := os.Executable()
	if err != nil {
		return "lucy"
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func SystemdDaemonServiceName() string { return "lucyd.service" }

func SystemdInstanceServiceName(name string) string {
	return "lucy-server@" + name + ".service"
}

func LaunchdDaemonLabel() string { return "org.mclucy.lucyd" }

func LaunchdInstanceLabel(name string) string {
	return "org.mclucy.server." + name
}

func systemdDir() string {
	if v := os.Getenv("LUCY_SYSTEMD_DIR"); v != "" {
		return v
	}
	if serviceDryRunEnabled() {
		return filepath.Join(StateDir(), "dry-run", "systemd")
	}
	return "/etc/systemd/system"
}

func launchdDir() string {
	if v := os.Getenv("LUCY_LAUNCHD_DIR"); v != "" {
		return v
	}
	if serviceDryRunEnabled() {
		return filepath.Join(StateDir(), "dry-run", "launchd")
	}
	return "/Library/LaunchDaemons"
}

type systemdManager struct{}

func (systemdManager) InstallDaemon(binary string) error {
	if err := os.MkdirAll(systemdDir(), 0o755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}
	if err := writeFile(filepath.Join(systemdDir(), SystemdDaemonServiceName()), []byte(renderSystemdDaemon(binary)), 0o644); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(systemdDir(), "lucy-server@.service"), []byte(renderSystemdInstanceTemplate(binary)), 0o644); err != nil {
		return err
	}
	_ = runCommand("systemctl", "daemon-reload")
	if err := (systemdManager{}).EnableDaemon(); err != nil {
		return err
	}
	return (systemdManager{}).StartDaemon()
}

func (systemdManager) InstallInstance(inst Instance, binary string) error {
	if err := os.MkdirAll(systemdDir(), 0o755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}
	if err := writeFile(filepath.Join(systemdDir(), "lucy-server@.service"), []byte(renderSystemdInstanceTemplate(binary)), 0o644); err != nil {
		return err
	}
	return runCommand("systemctl", "daemon-reload")
}

func (systemdManager) RemoveInstance(inst Instance) error {
	_ = (systemdManager{}).DisableInstance(inst)
	return runCommand("systemctl", "daemon-reload")
}

func (systemdManager) EnableDaemon() error {
	return runCommand("systemctl", "enable", SystemdDaemonServiceName())
}

func (systemdManager) StartDaemon() error {
	return runCommand("systemctl", "start", SystemdDaemonServiceName())
}

func (systemdManager) EnableInstance(inst Instance) error {
	return runCommand("systemctl", "enable", inst.SystemdService)
}

func (systemdManager) DisableInstance(inst Instance) error {
	return runCommand("systemctl", "disable", inst.SystemdService)
}

func (systemdManager) StartInstance(inst Instance) error {
	return runCommand("systemctl", "start", inst.SystemdService)
}

func (systemdManager) StopInstance(inst Instance) error {
	return runCommand("systemctl", "stop", inst.SystemdService)
}

func (systemdManager) RestartInstance(inst Instance) error {
	return runCommand("systemctl", "restart", inst.SystemdService)
}

func (systemdManager) StatusDaemon() ServiceState {
	return systemdStatus(SystemdDaemonServiceName())
}

func (systemdManager) StatusInstance(inst Instance) ServiceState {
	return systemdStatus(inst.SystemdService)
}

func systemdStatus(unit string) ServiceState {
	return ServiceState{
		Installed: systemctlOK("status", unit),
		Enabled:   systemctlOK("is-enabled", unit),
		Running:   systemctlOK("is-active", unit),
		NativeID:  unit,
		Manager:   "systemd",
		Detail:    systemctlText("show", "--property=ActiveState,SubState", "--value", unit),
	}
}

func systemctlOK(args ...string) bool {
	if serviceDryRunEnabled() {
		return false
	}
	cmd := exec.Command("systemctl", args...)
	return cmd.Run() == nil
}

func systemctlText(args ...string) string {
	if serviceDryRunEnabled() {
		return ""
	}
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type launchdManager struct{}

func (launchdManager) InstallDaemon(binary string) error {
	if err := os.MkdirAll(launchdDir(), 0o755); err != nil {
		return fmt.Errorf("create launchd directory: %w", err)
	}
	path := filepath.Join(launchdDir(), LaunchdDaemonLabel()+".plist")
	if err := writeFile(path, []byte(renderLaunchdDaemon(binary)), 0o644); err != nil {
		return err
	}
	_ = runCommand("launchctl", "bootstrap", "system", path)
	_ = (launchdManager{}).EnableDaemon()
	return (launchdManager{}).StartDaemon()
}

func (launchdManager) InstallInstance(inst Instance, binary string) error {
	if err := os.MkdirAll(launchdDir(), 0o755); err != nil {
		return fmt.Errorf("create launchd directory: %w", err)
	}
	path := filepath.Join(launchdDir(), inst.LaunchdLabel+".plist")
	if err := writeFile(path, []byte(renderLaunchdInstance(inst, binary)), 0o644); err != nil {
		return err
	}
	_ = runCommand("launchctl", "bootstrap", "system", path)
	return nil
}

func (launchdManager) RemoveInstance(inst Instance) error {
	_ = runCommand("launchctl", "bootout", "system/"+inst.LaunchdLabel)
	path := filepath.Join(launchdDir(), inst.LaunchdLabel+".plist")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	return nil
}

func (launchdManager) EnableDaemon() error {
	return runCommand("launchctl", "enable", "system/"+LaunchdDaemonLabel())
}

func (launchdManager) StartDaemon() error {
	return runCommand("launchctl", "kickstart", "-k", "system/"+LaunchdDaemonLabel())
}

func (launchdManager) EnableInstance(inst Instance) error {
	return runCommand("launchctl", "enable", "system/"+inst.LaunchdLabel)
}

func (launchdManager) DisableInstance(inst Instance) error {
	return runCommand("launchctl", "disable", "system/"+inst.LaunchdLabel)
}

func (launchdManager) StartInstance(inst Instance) error {
	return runCommand("launchctl", "kickstart", "-k", "system/"+inst.LaunchdLabel)
}

func (launchdManager) StopInstance(inst Instance) error {
	return runCommand("launchctl", "kill", "TERM", "system/"+inst.LaunchdLabel)
}

func (launchdManager) RestartInstance(inst Instance) error {
	if err := (launchdManager{}).StopInstance(inst); err != nil {
		return err
	}
	return (launchdManager{}).StartInstance(inst)
}

func (launchdManager) StatusDaemon() ServiceState {
	return launchdStatus(LaunchdDaemonLabel())
}

func (launchdManager) StatusInstance(inst Instance) ServiceState {
	return launchdStatus(inst.LaunchdLabel)
}

func launchdStatus(label string) ServiceState {
	out := commandText("launchctl", "print", "system/"+label)
	running := strings.Contains(out, "state = running") || strings.Contains(out, "pid =")
	return ServiceState{
		Installed: out != "",
		Enabled:   !strings.Contains(out, "disabled = true"),
		Running:   running,
		NativeID:  label,
		Manager:   "launchd",
		Detail:    firstLine(out),
	}
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	return state.AtomicWrite(path, data, mode)
}

func runCommand(name string, args ...string) error {
	if serviceDryRunEnabled() {
		return nil
	}
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func commandText(name string, args ...string) string {
	if serviceDryRunEnabled() {
		return ""
	}
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return s
}
