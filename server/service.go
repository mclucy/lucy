package server

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/mclucy/lucy/state"
)

// ServiceState is the native supervisor's view of installation and lifecycle state.
type ServiceState struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	NativeID  string `json:"native_id"`
	Manager   string `json:"manager"`
	Detail    string `json:"detail,omitempty"`
}

// DaemonInstaller installs the shared daemon's native service definition.
type DaemonInstaller interface {
	InstallDaemon(binary string) error
}

// DaemonEnabler configures the shared daemon to be available after boot.
type DaemonEnabler interface {
	EnableDaemon() error
}

// DaemonStarter starts the shared daemon through the native supervisor.
type DaemonStarter interface {
	StartDaemon() error
}

// DaemonStatusReader queries the native supervisor's view of the shared daemon.
type DaemonStatusReader interface {
	StatusDaemon() ServiceState
}

// InstanceInstaller installs a server's native service definition without starting it.
type InstanceInstaller interface {
	InstallInstance(inst Instance, binary string) error
}

// InstanceRemover removes a server from native service management.
type InstanceRemover interface {
	RemoveInstance(inst Instance) error
}

// InstanceEnabler enables a server's native service for future boots.
type InstanceEnabler interface {
	EnableInstance(inst Instance) error
}

// InstanceDisabler disables a server's native service for future boots.
type InstanceDisabler interface {
	DisableInstance(inst Instance) error
}

// InstanceStarter starts a server through its native supervisor.
type InstanceStarter interface {
	StartInstance(inst Instance) error
}

// InstanceStopper requests a server stop through its native supervisor.
type InstanceStopper interface {
	StopInstance(inst Instance) error
}

// InstanceRestarter restarts a server through its native supervisor.
type InstanceRestarter interface {
	RestartInstance(inst Instance) error
}

// InstanceStatusReader queries the native supervisor's view of a server.
type InstanceStatusReader interface {
	StatusInstance(inst Instance) ServiceState
}

// ServiceManager preserves the factory and CLI's complete lifecycle contract.
// Both native supervisors must implement every capability; consumers may compose
// narrower interfaces without treating required lifecycle operations as optional.
type ServiceManager interface {
	DaemonInstaller
	DaemonEnabler
	DaemonStarter
	DaemonStatusReader
	InstanceInstaller
	InstanceRemover
	InstanceEnabler
	InstanceDisabler
	InstanceStarter
	InstanceStopper
	InstanceRestarter
	InstanceStatusReader
}

// NewServiceManager selects the native service manager for the current platform.
func NewServiceManager() ServiceManager {
	switch runtime.GOOS {
	case "darwin":
		return launchdManager{}
	default:
		return systemdManager{}
	}
}

// CurrentBinary locates the running Lucy executable, resolving symlinks when possible.
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

// SystemdDaemonServiceName returns the shared daemon's systemd unit name.
func SystemdDaemonServiceName() string { return "lucyd.service" }

// SystemdInstanceServiceName returns an instance of the Lucy server unit template.
func SystemdInstanceServiceName(name string) string {
	return "lucy-server@" + name + ".service"
}

// LaunchdDaemonLabel returns the shared daemon's launchd identifier.
func LaunchdDaemonLabel() string { return "org.mclucy.lucyd" }

// LaunchdInstanceLabel returns the launchd identifier for a registered server.
func LaunchdInstanceLabel(name string) string {
	return "org.mclucy.server." + name
}

// systemdDir chooses the native unit directory or the configured test directory.
func systemdDir() string {
	if v := os.Getenv("LUCY_SYSTEMD_DIR"); v != "" {
		return v
	}
	if serviceDryRunEnabled() {
		return filepath.Join(StateDir(), "dry-run", "systemd")
	}
	return "/etc/systemd/system"
}

// launchdDir chooses the native plist directory or the configured test directory.
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

// InstallDaemon writes both systemd units and enables and starts the shared daemon.
func (systemdManager) InstallDaemon(binary string) error {
	binary, err := resolveSystemdBinary(binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(systemdDir(), 0o755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}
	if err := writeFile(filepath.Join(systemdDir(), SystemdDaemonServiceName()), []byte(renderSystemdDaemon(binary)), 0o644); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(systemdDir(), "lucy-server@.service"), []byte(renderSystemdInstanceTemplate(binary)), 0o644); err != nil {
		return err
	}
	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := (systemdManager{}).EnableDaemon(); err != nil {
		return err
	}
	return (systemdManager{}).StartDaemon()
}

// InstallInstance refreshes the systemd template without starting an instance.
func (systemdManager) InstallInstance(inst Instance, binary string) error {
	binary, err := resolveSystemdBinary(binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(systemdDir(), 0o755); err != nil {
		return fmt.Errorf("create systemd directory: %w", err)
	}
	if err := writeFile(filepath.Join(systemdDir(), "lucy-server@.service"), []byte(renderSystemdInstanceTemplate(binary)), 0o644); err != nil {
		return err
	}
	return runCommand("systemctl", "daemon-reload")
}

// RemoveInstance stops and disables the instance before its registration is removed.
func (systemdManager) RemoveInstance(inst Instance) error {
	if err := (systemdManager{}).StopInstance(inst); err != nil {
		return err
	}
	if err := (systemdManager{}).DisableInstance(inst); err != nil {
		return err
	}
	return runCommand("systemctl", "daemon-reload")
}

// EnableDaemon enables the shared systemd daemon at boot.
func (systemdManager) EnableDaemon() error {
	return runCommand("systemctl", "enable", SystemdDaemonServiceName())
}

// StartDaemon starts the shared systemd daemon if it is not already running.
func (systemdManager) StartDaemon() error {
	return runCommand("systemctl", "start", SystemdDaemonServiceName())
}

// EnableInstance enables the systemd instance at boot without starting it.
func (systemdManager) EnableInstance(inst Instance) error {
	return runCommand("systemctl", "enable", inst.SystemdService)
}

// DisableInstance removes the systemd instance's boot enablement without stopping it.
func (systemdManager) DisableInstance(inst Instance) error {
	return runCommand("systemctl", "disable", inst.SystemdService)
}

// StartInstance starts the registered systemd unit.
func (systemdManager) StartInstance(inst Instance) error {
	return runCommand("systemctl", "start", inst.SystemdService)
}

// StopInstance waits for systemd to finish stopping the registered unit.
func (systemdManager) StopInstance(inst Instance) error {
	return runCommand("systemctl", "stop", inst.SystemdService)
}

// RestartInstance delegates the stop and start sequence to systemd.
func (systemdManager) RestartInstance(inst Instance) error {
	return runCommand("systemctl", "restart", inst.SystemdService)
}

// StatusDaemon reports the shared systemd daemon's current status.
func (systemdManager) StatusDaemon() ServiceState {
	return systemdStatus(SystemdDaemonServiceName())
}

// StatusInstance reports the registered systemd instance's current status.
func (systemdManager) StatusInstance(inst Instance) ServiceState {
	return systemdStatus(inst.SystemdService)
}

// systemdStatus combines native status, enablement and activity probes for a unit.
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

// systemctlOK reports whether a systemctl query succeeds; dry runs report false.
func systemctlOK(args ...string) bool {
	if serviceDryRunEnabled() {
		return false
	}
	cmd := exec.Command("systemctl", args...)
	return cmd.Run() == nil
}

// systemctlText returns trimmed query output, or an empty string on probe failure.
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

// InstallDaemon reloads the launchd definition and lets its RunAtLoad policy start it.
func (launchdManager) InstallDaemon(binary string) error {
	binary, err := resolveServiceBinary(binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(launchdDir(), 0o755); err != nil {
		return fmt.Errorf("create launchd directory: %w", err)
	}
	path := filepath.Join(launchdDir(), LaunchdDaemonLabel()+".plist")
	if err := writeFile(path, []byte(renderLaunchdDaemon(binary)), 0o644); err != nil {
		return err
	}
	if launchdLoaded(LaunchdDaemonLabel()) {
		if err := runCommand("launchctl", "bootout", "system/"+LaunchdDaemonLabel()); err != nil {
			return err
		}
	}
	// A disabled label cannot be bootstrapped, even when its plist is present.
	if err := (launchdManager{}).EnableDaemon(); err != nil {
		return err
	}
	return runCommand("launchctl", "bootstrap", "system", path)
}

// InstallInstance writes a launchd plist without loading or starting the server.
func (launchdManager) InstallInstance(inst Instance, binary string) error {
	binary, err := resolveServiceBinary(binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(launchdDir(), 0o755); err != nil {
		return fmt.Errorf("create launchd directory: %w", err)
	}
	path := filepath.Join(launchdDir(), inst.LaunchdLabel+".plist")
	if err := writeFile(path, []byte(renderLaunchdInstance(inst, binary)), 0o644); err != nil {
		return err
	}
	return nil
}

// RemoveInstance unloads the launchd instance and removes its persisted definition.
func (launchdManager) RemoveInstance(inst Instance) error {
	_ = runCommand("launchctl", "bootout", "system/"+inst.LaunchdLabel)
	path := filepath.Join(launchdDir(), inst.LaunchdLabel+".plist")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launchd plist: %w", err)
	}
	return nil
}

// EnableDaemon clears launchd's persistent disabled flag for the shared daemon.
func (launchdManager) EnableDaemon() error {
	return runCommand("launchctl", "enable", "system/"+LaunchdDaemonLabel())
}

// StartDaemon forces the loaded launchd daemon to start, replacing a running process.
func (launchdManager) StartDaemon() error {
	return runCommand("launchctl", "kickstart", "-k", "system/"+LaunchdDaemonLabel())
}

// EnableInstance clears launchd's persistent disabled flag for the server label.
func (launchdManager) EnableInstance(inst Instance) error {
	return runCommand("launchctl", "enable", "system/"+inst.LaunchdLabel)
}

// DisableInstance prevents future loading of the launchd server label.
func (launchdManager) DisableInstance(inst Instance) error {
	return runCommand("launchctl", "disable", "system/"+inst.LaunchdLabel)
}

// StartInstance loads a new launchd job or kickstarts its existing definition.
func (launchdManager) StartInstance(inst Instance) error {
	if !launchdStatus(inst.LaunchdLabel).Installed {
		path := filepath.Join(launchdDir(), inst.LaunchdLabel+".plist")
		return runCommand("launchctl", "bootstrap", "system", path)
	}
	return runCommand("launchctl", "kickstart", "-k", "system/"+inst.LaunchdLabel)
}

// StopInstance sends SIGTERM to the process managed by the launchd server job.
func (launchdManager) StopInstance(inst Instance) error {
	return runCommand("launchctl", "kill", "TERM", "system/"+inst.LaunchdLabel)
}

// RestartInstance requests termination and then starts the launchd server job again.
func (launchdManager) RestartInstance(inst Instance) error {
	if err := (launchdManager{}).StopInstance(inst); err != nil {
		return err
	}
	return (launchdManager{}).StartInstance(inst)
}

// StatusDaemon reports the shared launchd daemon's current status.
func (launchdManager) StatusDaemon() ServiceState {
	return launchdStatus(LaunchdDaemonLabel())
}

// StatusInstance reports the registered launchd server job's current status.
func (launchdManager) StatusInstance(inst Instance) ServiceState {
	return launchdStatus(inst.LaunchdLabel)
}

// launchdLoaded uses the query exit status without parsing launchctl's diagnostic format.
func launchdLoaded(label string) bool {
	if serviceDryRunEnabled() {
		return false
	}
	return exec.Command("launchctl", "print", "system/"+label).Run() == nil
}

// launchdStatus extracts display status from launchctl's diagnostic output.
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

// resolveSystemdBinary also rejects executable paths that systemd refuses after unquoting.
func resolveSystemdBinary(binary string) (string, error) {
	path, err := resolveServiceBinary(binary)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(path) || strings.ContainsAny(path, "\"'\\*?[]") || strings.ContainsFunc(path, func(c rune) bool {
		return c < 0x20 || c == 0x7f
	}) {
		return "", fmt.Errorf("systemd executable path %q must be valid UTF-8 without quotes, backslashes, wildcards or control characters", path)
	}
	return path, nil
}

// resolveServiceBinary resolves caller-relative paths and PATH names before installation.
// Native service managers must not depend on the installer's working directory or PATH.
func resolveServiceBinary(binary string) (string, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("resolve service executable %q: %w", binary, err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve service executable %q: %w", binary, err)
	}
	return path, nil
}

// writeFile creates parent directories and atomically replaces a native service definition.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	return state.AtomicWrite(path, data, mode)
}

// runCommand runs a native service operation and preserves its stderr on failure.
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

// commandText returns trimmed query output, or an empty string on probe failure.
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

// firstLine returns the first non-padded line for compact native status display.
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
