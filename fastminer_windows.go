//go:build windows

package main

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The fast miner must never outlive this program. On Windows it is put in a
// "job" that the system closes, ending the miner with it, when this program
// exits for any reason, including being closed or killed.

var (
	childJobOnce sync.Once
	childJob     windows.Handle
)

func prepareChildProcess(command *exec.Cmd) {}

func bindChildProcess(command *exec.Cmd) {
	childJobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
		}
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			return
		}
		childJob = job
	})
	if childJob == 0 || command.Process == nil {
		return
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(process)
	_ = windows.AssignProcessToJobObject(childJob, process)
}
