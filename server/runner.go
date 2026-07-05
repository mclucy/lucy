package server

import (
	"bufio"
	"context"
	"encoding/json"
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
)

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

func (r *Runner) run(ctx context.Context) error {
	if err := os.MkdirAll(RunnerSocketDir(), 0o755); err != nil {
		return fmt.Errorf("create runner socket directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.cfg.Logs.ConsolePath), 0o755); err != nil {
		return fmt.Errorf("create console log directory: %w", err)
	}

	logFile, err := os.OpenFile(r.cfg.Logs.ConsolePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open console log: %w", err)
	}
	r.logFile = logFile
	defer logFile.Close()

	listener, err := listenUnix(RunnerSocketPath(r.name), 0o660)
	if err != nil {
		return fmt.Errorf("listen on runner socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(RunnerSocketPath(r.name))

	cmd := exec.CommandContext(ctx, r.cfg.Command, r.cfg.Args...)
	cmd.Dir = r.cfg.WorkingDir
	cmd.Env = mergeEnv(os.Environ(), r.cfg.Env)
	if err := applyRunUser(cmd, r.inst.RunUser); err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open server stdin: %w", err)
	}
	out := io.MultiWriter(os.Stdout, logFile)
	errOut := io.MultiWriter(os.Stderr, logFile)
	cmd.Stdout = out
	cmd.Stderr = errOut

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start minecraft server: %w", err)
	}
	r.mu.Lock()
	r.cmd = cmd
	r.stdin = stdin
	r.mu.Unlock()

	_ = MarkPendingRestart(r.name, false, "")
	r.register()

	go r.serveControl(ctx, listener)
	r.forwardSignals()

	err = cmd.Wait()
	if err != nil && !r.graceful {
		return err
	}
	return nil
}

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

func (r *Runner) handleControl(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&req); err != nil {
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

func (r *Runner) status() RunnerStatus {
	return RunnerStatus{
		Connected: true,
		Pid:       r.pid(),
		LogPath:   r.cfg.Logs.ConsolePath,
		StartedAt: r.startedAt,
	}
}

func (r *Runner) pid() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil || r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

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

func (r *Runner) stopGracefully() error {
	r.graceful = true
	if err := r.writeLine(r.cfg.Stop.Command); err != nil {
		return err
	}
	timeout, err := time.ParseDuration(r.cfg.Stop.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 60 * time.Second
	}
	go func() {
		time.Sleep(timeout)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.cmd != nil && r.cmd.Process != nil {
			_ = r.cmd.Process.Signal(syscall.SIGTERM)
		}
	}()
	return nil
}

func (r *Runner) forwardSignals() {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-signals
		_ = r.stopGracefully()
	}()
}

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
