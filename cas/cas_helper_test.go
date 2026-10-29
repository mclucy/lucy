package cas

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cross-process tests re-execute this binary as an isolated cas helper process.
// The parent sends one JSON request on stdin and reads JSON response lines on
// stdout, so every handshake is deterministic and no test depends on timing.
const (
	casHelperEnvVar    = "CAS_HELPER"
	casHelperOpEnvVar  = "CAS_HELPER_OP"
	casHelperCrashCode = 7
)

func TestMain(m *testing.M) {
	if os.Getenv(casHelperEnvVar) != "" {
		os.Exit(executeCASHelper(os.Getenv(casHelperOpEnvVar), os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

type casHelperRequest struct {
	Op      string          `json:"op"`
	Root    string          `json:"root"`
	Content []byte          `json:"content,omitempty"`
	Items   []casHelperItem `json:"items,omitempty"`
}

type casHelperItem struct {
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

type casHelperResponse struct {
	Ok      bool              `json:"ok"`
	Objects []casHelperObject `json:"objects,omitempty"`
	Stage   string            `json:"stage,omitempty"`
	Lease   string            `json:"lease,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type casHelperObject struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

func executeCASHelper(op string, stdin *os.File, stdout *os.File) int {
	lines := bufio.NewReader(stdin)
	request, ok := readCASHelperRequest(lines)
	if !ok {
		return 1
	}
	store, err := New(request.Root)
	if err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	switch op {
	case "put":
		return casHelperPut(store, request, stdout)
	case "hold":
		return casHelperHold(store, request, lines, stdout)
	case "orphan":
		return casHelperOrphan(store, request, stdout)
	default:
		return writeCASHelperResponse(stdout, casHelperResponse{Error: "unknown op " + op})
	}
}

func casHelperPut(store *Store, request casHelperRequest, stdout *os.File) int {
	response := casHelperResponse{Ok: true}
	for _, item := range request.Items {
		object, err := store.Put(bytes.NewReader(item.Content), nil)
		if err != nil {
			return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
		}
		response.Objects = append(response.Objects, casHelperObject{
			Name:   item.Name,
			Digest: object.Digest.String(),
			Size:   object.Size,
		})
	}
	if err := store.Close(); err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	return writeCASHelperResponse(stdout, response)
}

// casHelperHold keeps a stage lease open until the parent releases the child.
// An abort release exercises staged cleanup, a crash release exercises the
// state a killed process leaves behind.
func casHelperHold(
	store *Store,
	request casHelperRequest,
	lines *bufio.Reader,
	stdout *os.File,
) int {
	writer, err := store.NewWriter()
	if err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	if _, err := writer.Write(request.Content); err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	if code := writeCASHelperResponse(stdout, casHelperResponse{
		Ok:    true,
		Stage: writer.dataName,
		Lease: writer.leaseName,
	}); code != 0 {
		return 1
	}
	release, err := readCASHelperRelease(lines)
	if err != nil {
		return 1
	}
	if release != "crash" {
		if err := writer.Abort(); err != nil {
			return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
		}
		return writeCASHelperResponse(stdout, casHelperResponse{Ok: true})
	}
	os.Exit(casHelperCrashCode)
	return 0
}

// casHelperOrphan deletes the lease of an unfinished stage, the data-only
// state a crash and a midway cleanup leave behind.
func casHelperOrphan(store *Store, request casHelperRequest, stdout *os.File) int {
	writer, err := store.NewWriter()
	if err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	if _, err := writer.Write(request.Content); err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	leasePath := filepath.Join(store.root.Name(), filepath.FromSlash(writer.leaseName))
	if err := os.Remove(leasePath); err != nil {
		return writeCASHelperResponse(stdout, casHelperResponse{Error: err.Error()})
	}
	return writeCASHelperResponse(stdout, casHelperResponse{
		Ok:    true,
		Stage: writer.dataName,
		Lease: writer.leaseName,
	})
}

func readCASHelperRequest(lines *bufio.Reader) (casHelperRequest, bool) {
	line, err := lines.ReadString('\n')
	if err != nil {
		return casHelperRequest{}, false
	}
	var request casHelperRequest
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		return casHelperRequest{}, false
	}
	return request, true
}

func readCASHelperRelease(lines *bufio.Reader) (string, error) {
	line, err := lines.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func writeCASHelperResponse(stdout *os.File, response casHelperResponse) int {
	data, err := json.Marshal(response)
	if err != nil {
		return 1
	}
	if _, err := stdout.Write(append(data, '\n')); err != nil {
		return 1
	}
	return 0
}

func runCASHelper(t *testing.T, request casHelperRequest) casHelperResponse {
	t.Helper()
	response, err := runCASHelperCommand(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func runCASHelperCommand(ctx context.Context, request casHelperRequest) (casHelperResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return casHelperResponse{}, fmt.Errorf("marshal CAS helper request: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return casHelperResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable)
	command.Env = append(os.Environ(), casHelperEnvVar+"=1", casHelperOpEnvVar+"="+request.Op, "GORACE=atexit_sleep_ms=0")
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return casHelperResponse{}, fmt.Errorf("CAS helper %s: %w (stderr: %s)", request.Op, err, stderr.String())
	}
	response, err := parseFirstCASHelperResponse(stdout.Bytes())
	if err != nil {
		return casHelperResponse{}, fmt.Errorf("read CAS helper %s: %w (stdout: %q, stderr: %s)", request.Op, err, stdout.String(), stderr.String())
	}
	if response.Error != "" {
		return response, fmt.Errorf("CAS helper %s failed: %s", request.Op, response.Error)
	}
	return response, nil
}

type heldCASHelper struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	reader  *bufio.Reader
	stderr  *bytes.Buffer
}

// holdCASHelperStage starts a helper that keeps one stage leased and returns
// only after the handshake response announces the leased stage.
func holdCASHelperStage(t *testing.T, request casHelperRequest) (casHelperResponse, heldCASHelper) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, casHelperExecutable(t))
	command.Env = append(os.Environ(), casHelperEnvVar+"=1", casHelperOpEnvVar+"="+request.Op, "GORACE=atexit_sleep_ms=0")
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal cas helper request: %v", err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open cas helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open cas helper stdout: %v", err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start cas helper: %v", err)
	}
	reader := bufio.NewReader(stdout)
	if _, err := stdin.Write(append(payload, '\n')); err != nil {
		t.Fatalf("send cas helper request: %v", err)
	}
	response, err := readCASHelperResponse(reader)
	if err != nil {
		t.Fatalf("cas helper handshake: %v (stderr: %s)", err, stderr.String())
	}
	if response.Error != "" {
		t.Fatalf("cas helper hold failed: %s", response.Error)
	}
	helper := heldCASHelper{command: command, stdin: stdin, reader: reader, stderr: stderr}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	return response, helper
}

// release drains the held helper and reports how the released process ended.
func (h heldCASHelper) release(t *testing.T, mode string, wantExitCode int) {
	t.Helper()
	if _, err := h.stdin.Write([]byte(mode + "\n")); err != nil {
		t.Fatalf("release cas helper: %v", err)
	}
	if err := h.stdin.Close(); err != nil {
		t.Fatalf("close cas helper stdin: %v", err)
	}
	if wantExitCode == 0 {
		response, err := readCASHelperResponse(h.reader)
		if err != nil || !response.Ok || response.Error != "" {
			t.Fatalf("read cas helper final response: %+v, %v", response, err)
		}
	}
	waitErr := h.command.Wait()
	if wantExitCode == 0 {
		if waitErr != nil {
			t.Fatalf("cas helper orderly exit: %v (stderr: %s)", waitErr, h.stderr.String())
		}
		return
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		t.Fatalf("cas helper crash exit: %v (stderr: %s)", waitErr, h.stderr.String())
	}
	if exitErr.ExitCode() != wantExitCode {
		t.Fatalf("cas helper crash exit code %d, want %d", exitErr.ExitCode(), wantExitCode)
	}
}

func readCASHelperResponse(reader *bufio.Reader) (casHelperResponse, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return casHelperResponse{}, err
	}
	var response casHelperResponse
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return casHelperResponse{}, err
	}
	return response, nil
}

func parseFirstCASHelperResponse(output []byte) (casHelperResponse, error) {
	if index := bytes.IndexByte(output, '\n'); index >= 0 {
		output = output[:index]
	}
	var response casHelperResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return casHelperResponse{}, err
	}
	return response, nil
}

func casHelperExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	return executable
}
