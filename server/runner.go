package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mclucy/lucy/log"
)

// Runner owns one Minecraft process, its console streams, and its control socket.
type Runner struct {
	name      string
	inst      Instance
	cfg       RuntimeConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	logFile   *os.File
	startedAt string
	graceful  bool
	mu        sync.Mutex
}

// RunServer loads a registered instance and supervises it until exit or cancellation.
// A missing runtime config is inferred from the instance directory.
func RunServer(ctx context.Context, name string) error {
	inst, err := requiredInstance(name)
	if err != nil {
		return err
	}
	cfg, err := ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return fmt.Errorf("read %s: %w", inst.RuntimeConfig, err)
	}
	if cfg == nil {
		guessed := GuessRuntimeConfig(inst.Root)
		cfg = &guessed
	}

	r := &Runner{
		name:      name,
		inst:      *inst,
		cfg:       *cfg,
		startedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return r.run(ctx)
}

// run prepares IPC and logs, starts the child with its configured credentials,
// and waits for exit even after requesting a graceful stop.
func (r *Runner) run(ctx context.Context) (err error) {
	if err := prepareRuntimeDirs(); err != nil {
		return fmt.Errorf("create runner socket directory: %w", err)
	}

	logFile, err := r.openConsoleLog()
	if err != nil {
		return err
	}
	r.logFile = logFile
	defer func() {
		if closeErr := logFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close console log: %w", closeErr)
		}
	}()

	listener, err := listenUnix(RunnerSocketPath(r.name), 0o600)
	if err != nil {
		return fmt.Errorf("listen on runner socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(RunnerSocketPath(r.name))

	cmd := exec.Command(r.cfg.Command, r.cfg.Args...)
	cmd.Dir = r.cfg.WorkingDir
	cmd.Env = mergeEnv(os.Environ(), r.cfg.Env)
	if err := prepareManagedProcess(cmd, r.inst.RunUser); err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open server stdin: %w", err)
	}
	cmd.Stdout = log.RawOutput(os.Stdout, logFile)
	cmd.Stderr = log.RawOutput(os.Stderr, logFile)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start minecraft server: %w", err)
	}
	r.mu.Lock()
	r.cmd = cmd
	r.stdin = stdin
	r.mu.Unlock()

	_ = NewRuntimeStateService().MarkPendingRestart(r.name, false, "")
	r.register()

	go r.serveControl(ctx, listener)
	r.forwardSignals()

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		_ = r.stopGracefully()
		err = <-waitErr
	}
	if err != nil && !r.isGraceful() {
		return err
	}
	return nil
}

// openConsoleLog confines the privileged log open to the registered instance
// root. os.Root keeps path resolution anchored to that directory even when its
// writable contents contain symlinks or are changed concurrently.
func (r *Runner) openConsoleLog() (*os.File, error) {
	instanceRoot, err := filepath.Abs(r.inst.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve server root: %w", err)
	}
	logPath := r.cfg.Logs.ConsolePath
	if !filepath.IsAbs(logPath) {
		return nil, fmt.Errorf("console log path must be absolute and inside server root %s", instanceRoot)
	}
	logPath, err = filepath.Abs(logPath)
	if err != nil {
		return nil, fmt.Errorf("resolve console log path: %w", err)
	}
	rel, err := filepath.Rel(instanceRoot, logPath)
	if err != nil {
		return nil, fmt.Errorf("resolve console log within server root: %w", err)
	}
	parentPrefix := ".." + string(os.PathSeparator)
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, parentPrefix) {
		return nil, fmt.Errorf("console log must be inside server root %s", instanceRoot)
	}

	root, err := os.OpenRoot(instanceRoot)
	if err != nil {
		return nil, fmt.Errorf("open server root: %w", err)
	}
	defer root.Close()
	if parent := filepath.Dir(rel); parent != "." {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return nil, fmt.Errorf("create console log directory: %w", err)
		}
	}
	logFile, err := root.OpenFile(rel, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open console log: %w", err)
	}
	info, err := logFile.Stat()
	if err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("stat console log: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = logFile.Close()
		return nil, fmt.Errorf("console log is not a regular file: %s", logPath)
	}
	r.cfg.Logs.ConsolePath = logPath
	return logFile, nil
}

// register announces the runner when the daemon is available; recovery can also
// discover its derived socket path if this best-effort announcement fails.
func (r *Runner) register() {
	_ = CallDaemon(context.Background(), Request{
		Op: OpRunnerRegister,
		Runner: RunnerRegistration{
			Name:       r.name,
			SocketPath: RunnerSocketPath(r.name),
			Pid:        r.pid(),
			LogPath:    r.cfg.Logs.ConsolePath,
			StartedAt:  r.startedAt,
		},
	}, nil)
}

// serveControl accepts independent console requests until cancellation or closure.
func (r *Runner) serveControl(ctx context.Context, listener net.Listener) {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go r.handleControl(conn)
	}
}

// handleControl processes one bounded request on the owner-only runner socket.
func (r *Runner) handleControl(conn net.Conn) {
	defer conn.Close()
	req, err := decodeRequest(conn)
	if err != nil {
		respond(conn, nil, fmt.Errorf("decode runner request: %w", err))
		return
	}
	switch req.Op {
	case OpRunnerStatus:
		respond(conn, r.status(), nil)
	case OpRunnerSend:
		respond(conn, nil, r.writeLine(req.Line))
	case OpRunnerStop:
		respond(conn, nil, r.stopGracefully())
	default:
		respond(conn, nil, fmt.Errorf("unknown runner operation %q", req.Op))
	}
}

// status reports the child PID and console metadata for this reachable runner.
func (r *Runner) status() RunnerStatus {
	return RunnerStatus{
		Connected: true,
		Pid:       r.pid(),
		LogPath:   r.cfg.Logs.ConsolePath,
		StartedAt: r.startedAt,
	}
}

// pid returns zero until the child has started, synchronizing access with startup.
func (r *Runner) pid() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil || r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

// writeLine removes trailing newlines and writes one nonempty console command.
func (r *Runner) writeLine(line string) error {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return fmt.Errorf("console command is required")
	}
	r.mu.Lock()
	stdin := r.stdin
	r.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("server stdin is not available")
	}
	_, err := io.WriteString(stdin, line+"\n")
	return err
}

// stopGracefully sends the configured stop command once and schedules escalation
// if the child outlives its grace period; callers still wait for process exit.
func (r *Runner) stopGracefully() error {
	timeout, err := time.ParseDuration(r.cfg.Stop.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 60 * time.Second
	}
	if !r.markGraceful() {
		return nil
	}
	if err := r.writeLine(r.cfg.Stop.Command); err != nil {
		r.scheduleForcedStop(0)
		return err
	}
	r.scheduleForcedStop(timeout)
	return nil
}

// markGraceful claims the one-time stop transition across signals and IPC callers.
func (r *Runner) markGraceful() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.graceful {
		return false
	}
	r.graceful = true
	return true
}

// isGraceful reports whether an operator stop should suppress child exit errors.
func (r *Runner) isGraceful() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.graceful
}

// scheduleForcedStop escalates from TERM to KILL if the child ignores shutdown.
func (r *Runner) scheduleForcedStop(timeout time.Duration) {
	go func() {
		time.Sleep(timeout)
		_ = r.signalIfRunning(syscall.SIGTERM)
		time.Sleep(10 * time.Second)
		_ = r.signalIfRunning(syscall.SIGKILL)
	}()
}

// forwardSignals translates the first termination signal into a console stop.
func (r *Runner) forwardSignals() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		_ = r.stopGracefully()
	}()
}

// signalIfRunning delivers a process-group signal only while the child is active.
func (r *Runner) signalIfRunning(signal os.Signal) error {
	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return nil
	}
	return signalManagedProcess(cmd, signal)
}

// mergeEnv appends configured overrides without mutating the inherited environment.
// os/exec keeps the last value when duplicate environment keys are present.
func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := append([]string(nil), base...)
	for key, value := range extra {
		merged = append(merged, key+"="+value)
	}
	return merged
}
