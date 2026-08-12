//go:build windows

// capture_joblimit_windows.go - making sure ffmpeg cannot outlive the app.
//
// # THE BUG THIS EXISTS TO KILL
//
// ffmpeg runs as a child process. On Windows, a child process does NOT die when
// its parent does. Quit SiegeIQ Sync, and the ffmpeg it launched carries on
// recording your screen, forever, with nothing in the interface to tell you and
// no way to stop it short of Task Manager.
//
// This was not hypothetical. On the machine this was written for, three
// orphaned ffmpeg processes were found still writing into the rolling buffer
// folder six minutes after the app had logged "stopping capture", one of them
// left over from a version that had been replaced an hour earlier. Three
// encoders were running during ranked matches. The buffer was three interleaved
// recordings pretending to be one, which is why saved clips stuttered. And the
// brand new "pause when Siege is not in front" feature appeared to do nothing,
// because the process it politely asked to stop was not the one still running.
//
// It is also, quietly, the worst thing in this whole application from a trust
// point of view. A recorder that keeps recording after you close it is spyware,
// regardless of intent.
//
// # WHY A JOB OBJECT RATHER THAN TIDIER SHUTDOWN CODE
//
// Stopping capture on exit is necessary but not sufficient, and the difference
// matters. Shutdown code only runs when the app gets to shut down. It does not
// run when the app is killed from Task Manager, when it crashes, when Windows
// force-closes it during a restart, or when a build overwrites a running exe.
// Every one of those leaves an orphan, and orphans accumulate silently.
//
// A Windows job object with KILL_ON_JOB_CLOSE moves the guarantee into the
// kernel. Every ffmpeg is assigned to the job; the job handle is owned by this
// process; when this process ends by ANY means, Windows closes the handle and
// terminates everything in the job. There is no code path that can skip it,
// including no code path at all.
//
// A PID file was the obvious cheaper alternative and it is worse in a way that
// is easy to miss: Windows recycles process IDs. Killing "the PID we wrote down
// last time" eventually kills a stranger's process, and that failure would be
// rare, unreproducible and awful.
package main

import (
	"syscall"
	"unsafe"
)

var (
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCreateJobObjectW       = modKernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObj   = modKernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObj  = modKernel32.NewProc("AssignProcessToJobObject")
	procOpenProcessForJobAssig = modKernel32.NewProc("OpenProcess")
)

const (
	jobObjectExtendedLimitInfoClass = 9
	jobObjectLimitKillOnJobClose    = 0x00002000

	// PROCESS_SET_QUOTA | PROCESS_TERMINATE, the two rights
	// AssignProcessToJobObject requires and nothing more. Asking for
	// PROCESS_ALL_ACCESS would work and would also be a wider handle than this
	// needs, on a process this app did not write.
	processSetQuota  = 0x0100
	processTerminate = 0x0001
)

// jobObjectBasicLimitInformation mirrors JOBOBJECT_BASIC_LIMIT_INFORMATION.
// Field order and widths are load-bearing: this struct is handed to the kernel
// by size, so a wrong layout is not a compile error, it is a silent failure to
// apply the limit.
type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	_                       uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	_                       uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// captureJob is created once, on first use, and deliberately never closed. Its
// lifetime IS the process lifetime, which is the entire mechanism: the handle
// closing is what kills the children, and the only thing that should close it
// is the process ending.
var captureJob syscall.Handle

// ensureCaptureJob creates the kill-on-close job, once.
//
// Returns 0 if the job could not be created. Callers treat that as "carry on
// without the safety net" rather than refusing to record: a machine with an
// unusual security policy that blocks job objects should still get a working
// recorder, and the ordinary shutdown path still stops ffmpeg there.
func ensureCaptureJob() syscall.Handle {
	if captureJob != 0 {
		return captureJob
	}

	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		logf("recorder: could not create the process group that stops ffmpeg outliving this app (%v)", err)
		return 0
	}

	var info jobObjectExtendedLimitInformation
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose

	ok, _, err := procSetInformationJobObj.Call(
		h,
		uintptr(jobObjectExtendedLimitInfoClass),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if ok == 0 {
		logf("recorder: could not arm the process group that stops ffmpeg outliving this app (%v)", err)
		syscall.CloseHandle(syscall.Handle(h))
		return 0
	}

	captureJob = syscall.Handle(h)
	return captureJob
}

// superviseProcess puts a freshly started ffmpeg into the job.
//
// Failures are logged and swallowed. A capture that records correctly but might
// outlive a crash is still better than no capture, and the log line is what
// makes the weaker guarantee visible rather than assumed.
func superviseProcess(pid int) {
	job := ensureCaptureJob()
	if job == 0 || pid <= 0 {
		return
	}

	h, _, err := procOpenProcessForJobAssig.Call(
		uintptr(processSetQuota|processTerminate), 0, uintptr(pid))
	if h == 0 {
		logf("recorder: could not attach ffmpeg (pid %d) to the process group (%v)", pid, err)
		return
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	if ok, _, err := procAssignProcessToJobObj.Call(uintptr(job), h); ok == 0 {
		logf("recorder: could not attach ffmpeg (pid %d) to the process group (%v)", pid, err)
	}
}
