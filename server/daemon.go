package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
)

// instanceController requires only the native server operations dispatched by Daemon.
// Installation and management of the shared daemon belong to the CLI instead.
type instanceController interface {
	InstanceStarter
	InstanceStopper
	InstanceRestarter
	InstanceEnabler
	InstanceDisabler
	InstanceStatusReader
}

// Daemon coordinates native services and forwards commands to instance runners.
type Daemon struct {
	mu      sync.RWMutex
	runners map[string]RunnerRegistration
	service instanceController
}

// RunDaemon accepts local IPC until ctx is canceled, keeping runners independent
// of the daemon's lifetime so an API restart does not stop Minecraft processes.
func RunDaemon(ctx context.Context) error {
	if err := prepareRuntimeDirs(); err != nil {
		return fmt.Errorf("create daemon runtime directory: %w", err)
	}
	if err := os.MkdirAll(LogDir(), 0o755); err != nil {
		return fmt.Errorf("create daemon log directory: %w", err)
	}

	listener, err := listenUnix(DaemonSocketPath(), 0o660)
	if err != nil {
		return fmt.Errorf("listen on daemon socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(DaemonSocketPath())

	d := &Daemon{
		runners: make(map[string]RunnerRegistration),
		service: NewServiceManager(),
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept daemon connection: %w", err)
		}
		go d.handle(conn)
	}
}

// handle receives and authorizes one bounded request before dispatching it.
func (d *Daemon) handle(conn net.Conn) {
	defer conn.Close()

	req, err := decodeRequest(conn)
	if err != nil {
		respond(conn, nil, fmt.Errorf("decode request: %w", err))
		return
	}
	if err := authorizeDaemonRequest(conn, req); err != nil {
		respond(conn, nil, err)
		return
	}
	data, err := d.dispatch(req)
	respond(conn, data, err)
}

// dispatch routes an authorized operation to the registry, runner, or native service.
func (d *Daemon) dispatch(req Request) (any, error) {
	switch req.Op {
	case OpList:
		return ListInstances()
	case OpStatus:
		return d.status(req.Instance)
	case OpStart:
		inst, err := requiredInstance(req.Instance)
		if err != nil {
			return nil, err
		}
		return nil, d.service.StartInstance(*inst)
	case OpStop:
		return nil, d.stop(req.Instance)
	case OpRestart:
		inst, err := requiredInstance(req.Instance)
		if err != nil {
			return nil, err
		}
		return nil, d.service.RestartInstance(*inst)
	case OpEnable:
		inst, err := requiredInstance(req.Instance)
		if err != nil {
			return nil, err
		}
		return nil, d.service.EnableInstance(*inst)
	case OpDisable:
		inst, err := requiredInstance(req.Instance)
		if err != nil {
			return nil, err
		}
		return nil, d.service.DisableInstance(*inst)
	case OpSend:
		return nil, d.send(req.Instance, req.Line)
	case OpRunnerRegister:
		if err := ValidateInstanceName(req.Runner.Name); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.runners[req.Runner.Name] = req.Runner
		d.mu.Unlock()
		return req.Runner, nil
	case OpPackageTask:
		return d.packageTask(req.Instance, req.Task)
	default:
		return nil, fmt.Errorf("unknown daemon operation %q", req.Op)
	}
}

// status combines persisted restart intent with current service and runner status.
func (d *Daemon) status(name string) (InstanceStatus, error) {
	inst, err := requiredInstance(name)
	if err != nil {
		return InstanceStatus{}, err
	}
	st, _ := NewRuntimeStateService().Read(inst.Name)
	return InstanceStatus{
		Instance:       *inst,
		Service:        d.service.StatusInstance(*inst),
		Runner:         d.runnerStatus(inst.Name),
		PendingRestart: st.PendingRestart,
		PendingReason:  st.Reason,
	}, nil
}

// runnerStatus probes the registered socket, falling back to the derived path
// after a daemon restart has discarded in-memory registrations.
func (d *Daemon) runnerStatus(name string) RunnerStatus {
	d.mu.RLock()
	reg, ok := d.runners[name]
	d.mu.RUnlock()
	if !ok {
		reg = RunnerRegistration{
			Name:       name,
			SocketPath: RunnerSocketPath(name),
		}
	}
	var st RunnerStatus
	if err := callUnix(context.Background(), reg.SocketPath, Request{Op: OpRunnerStatus, Instance: name}, &st); err != nil {
		return RunnerStatus{Connected: false, Detail: err.Error()}
	}
	return st
}

// send forwards a nonempty console command to the instance's runner.
func (d *Daemon) send(name, line string) error {
	if line == "" {
		return fmt.Errorf("console command is required")
	}
	reg := d.runnerRegistration(name)
	return callUnix(context.Background(), reg.SocketPath, Request{
		Op:       OpRunnerSend,
		Instance: name,
		Line:     line,
	}, nil)
}

// stop requests a graceful runner stop, using native service control if IPC fails.
func (d *Daemon) stop(name string) error {
	reg := d.runnerRegistration(name)
	if err := callUnix(context.Background(), reg.SocketPath, Request{
		Op:       OpRunnerStop,
		Instance: name,
	}, nil); err == nil {
		return nil
	}
	inst, err := requiredInstance(name)
	if err != nil {
		return err
	}
	return d.service.StopInstance(*inst)
}

// runnerRegistration retrieves a live registration or derives the recovery endpoint.
func (d *Daemon) runnerRegistration(name string) RunnerRegistration {
	d.mu.RLock()
	reg, ok := d.runners[name]
	d.mu.RUnlock()
	if ok {
		return reg
	}
	return RunnerRegistration{Name: name, SocketPath: RunnerSocketPath(name)}
}

// packageTask serializes package changes per instance and marks running servers
// for restart, distinguishing persistence failures from failure of the task itself.
func (d *Daemon) packageTask(
	name string,
	task PackageTaskRequest,
) (PackageTaskResult, error) {
	inst, err := requiredInstance(name)
	if err != nil {
		return PackageTaskResult{}, err
	}

	var result PackageTaskResult
	err = WithInstanceLock(inst.Name, func() error {
		var taskErr error
		result, taskErr = RunPackageTask(context.Background(), *inst, task)
		return taskErr
	})
	if err != nil {
		return result, err
	}

	if d.service.StatusInstance(*inst).Running {
		if err := NewRuntimeStateService().MarkPendingRestart(inst.Name, true, pendingRestartReason(task.Name)); err != nil {
			return result, fmt.Errorf("package task %s completed, but could not save restart status: %w; restart server %q manually (do not repeat the package task)", task.Name, err, inst.Name)
		}
	}
	return result, nil
}

// pendingRestartReason explains which part of package state a completed task changed.
func pendingRestartReason(taskName string) string {
	switch taskName {
	case TaskAdd:
		return "package files changed"
	case TaskInstall:
		return "install changed runtime files"
	case TaskRemove:
		return "package intent changed"
	default:
		return "server package state changed"
	}
}

// requiredInstance validates a name and rejects missing registry entries.
func requiredInstance(name string) (*Instance, error) {
	if name == "" {
		return nil, fmt.Errorf("server name is required")
	}
	inst, err := ReadInstance(name)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("server %q is not registered", name)
	}
	return inst, nil
}
