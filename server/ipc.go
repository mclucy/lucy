package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

const (
	OpList           = "server.list"
	OpStatus         = "server.status"
	OpStart          = "server.start"
	OpStop           = "server.stop"
	OpRestart        = "server.restart"
	OpEnable         = "server.enable"
	OpDisable        = "server.disable"
	OpSend           = "server.send"
	OpRunnerRegister = "runner.register"
	OpRunnerStatus   = "runner.status"
	OpRunnerSend     = "runner.send"
	OpRunnerStop     = "runner.stop"
	OpPackageTask    = "package.task"
)

// ErrIPCUnavailable marks a request that failed before an IPC connection was
// established. Callers may safely retry only this class of transport failure.
var ErrIPCUnavailable = errors.New("IPC endpoint unavailable")

type Request struct {
	Op       string             `json:"op"`
	Instance string             `json:"instance,omitempty"`
	Line     string             `json:"line,omitempty"`
	Runner   RunnerRegistration `json:"runner,omitempty"`
	Task     PackageTaskRequest `json:"task,omitempty"`
}

type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

type ResponseError struct {
	Message string
}

func (e ResponseError) Error() string { return e.Message }

type RunnerRegistration struct {
	Name       string `json:"name"`
	SocketPath string `json:"socket_path"`
	Pid        int    `json:"pid"`
	LogPath    string `json:"log_path"`
	StartedAt  string `json:"started_at"`
}

type RunnerStatus struct {
	Connected bool   `json:"connected"`
	Pid       int    `json:"pid,omitempty"`
	LogPath   string `json:"log_path,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type InstanceStatus struct {
	Instance       Instance     `json:"instance"`
	Service        ServiceState `json:"service"`
	Runner         RunnerStatus `json:"runner"`
	PendingRestart bool         `json:"pending_restart"`
	PendingReason  string       `json:"pending_reason,omitempty"`
}

type PackageTaskRequest struct {
	Name            string             `json:"name"`
	Args            []string           `json:"args,omitempty"`
	Platform        string             `json:"platform,omitempty"`
	UseGitHubMirror bool               `json:"use_github_mirror,omitempty"`
	AddOptions      PackageTaskAddOpts `json:"add_options,omitempty"`
}

type PackageTaskAddOpts struct {
	Force        bool `json:"force,omitempty"`
	WithOptional bool `json:"with_optional,omitempty"`
	NoOptional   bool `json:"no_optional,omitempty"`
}

type PackageTaskResult struct {
	Output string `json:"output,omitempty"`
}

func CallDaemon(ctx context.Context, req Request, out any) error {
	return callUnix(ctx, DaemonSocketPath(), req, out)
}

func callUnix(ctx context.Context, socketPath string, req Request, out any) error {
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIPCUnavailable, err)
	}
	defer conn.Close()

	var deadline time.Time
	if value, ok := ctx.Deadline(); ok {
		deadline = value
	} else if req.Op != OpPackageTask {
		deadline = time.Now().Add(30 * time.Second)
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}
	stopCancel := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	defer stopCancel()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("send request: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "daemon request failed"
		}
		return ResponseError{Message: resp.Error}
	}
	if out != nil && len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func respond(conn net.Conn, data any, err error) {
	resp := Response{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	} else if data != nil {
		raw, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			resp.OK = false
			resp.Error = marshalErr.Error()
		} else {
			resp.Data = raw
		}
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

func listenUnix(socketPath string, mode os.FileMode) (net.Listener, error) {
	if err := ensureSharedDir(filepathDir(socketPath), 0o775); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, mode); err != nil {
		_ = l.Close()
		return nil, err
	}
	_ = chownGroup(socketPath, DefaultGroup)
	return l, nil
}

func filepathDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}
