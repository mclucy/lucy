//go:build e2e && windows

package mutation

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// osMissingFileText is Windows' message for a missing file, as Lucy's error
// output carries it.
const osMissingFileText = "cannot find the file"

// jobBasicAccountingInformation mirrors JOBOBJECT_BASIC_ACCOUNTING_INFORMATION,
// which x/sys/windows does not define.
type jobBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// processTree is the job object that a started command is assigned to.
type processTree struct {
	job windows.Handle
}

// startProcessTree starts command and assigns it to a new job object. The job
// kills its members when its handle closes. Context cancellation terminates the
// job. Only processes in the job are observed: a child the leader starts before
// the assignment is outside the job, and a process that breaks away from the job
// is not reached.
func startProcessTree(command *exec.Cmd) (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job object: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job object: %w", err)
	}
	command.Cancel = func() error {
		return windows.TerminateJobObject(job, 1)
	}
	if err := command.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	var assignErr error
	handleErr := command.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
	})
	if err := errors.Join(handleErr, assignErr); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assign process to job object: %w", err)
	}
	return &processTree{job: job}, nil
}

// stop terminates the job's remaining members and waits, within
// processTreeDrainTimeout, until the job reports none active. It then closes the
// job handle, which kills anything still in the job. killed reports whether a
// member was active when stop began.
func (t *processTree) stop() (killed bool, err error) {
	defer func() {
		if closeErr := windows.CloseHandle(t.job); err == nil && closeErr != nil {
			err = fmt.Errorf("close job object: %w", closeErr)
		}
	}()
	active, err := activeProcesses(t.job)
	if err != nil {
		return false, err
	}
	if active == 0 {
		return false, nil
	}
	if err := windows.TerminateJobObject(t.job, 1); err != nil {
		return true, fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(processTreeDrainTimeout)
	for {
		active, err = activeProcesses(t.job)
		if err != nil {
			return true, err
		}
		if active == 0 {
			return true, nil
		}
		if time.Now().After(deadline) {
			return true, fmt.Errorf("job object still has %d active processes %s after termination",
				active, processTreeDrainTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func activeProcesses(job windows.Handle) (uint32, error) {
	var info jobBasicAccountingInformation
	var returned uint32
	if err := windows.QueryInformationJobObject(job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &returned); err != nil {
		return 0, fmt.Errorf("query job object: %w", err)
	}
	return info.ActiveProcesses, nil
}
