// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// The package provides helpers for invoking strace to trace the snap execution.
// Since bootstrapping a snap environment involves running a privileged binary,
// the tracing is composed of 2 parts:
//
// - tracee, whose 'lifetime' begins as the first stage of the bootstrap, right
// - after transitioning from `snap run`. This will eventually transition into
// - the desired snap application.
// - tracing application, strace, which attaches to the tracee's execution chain
//
// Running strace as a standalone process which only attaches to the traced
// application has a significant advantage, bootstrapping and the application
// runs in a pristine environment and continues execution in the security
// context of a regular user, in the assigned snap cgroup.
//
// The usual entry point doing all the orchestration is `snap run`. The
// execution injects an additional synchronization step to properly identify a
// moment when the tracee execution check is ready to attach strace and next
// when strace successfully attached to the traced process. The synchronization
// is provided by snap-strace-shim, which has the purpose of raising SIGSTOP,
// which provides the first synchronization point, after which snap run launches
// strace and processes its stderr to identify a moment when strace attached
// to the process held in SIGSTOP handler. At this point sending SIGCONT will
// unblock further execution.
package strace

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// These syscalls are excluded because they make strace hang on all or
// some architectures (gettimeofday on arm64).
// Furthermore not all syscalls exist on all arches, and for some
// arches strace has a mapping for some of the non-existent
// syscalls. Thus there is no universal list of syscalls to exclude.
func getExcludedSyscalls() string {
	switch runtime.GOARCH {
	case "arm", "arm64", "riscv64":
		return "!pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday,nanosleep"
	default:
		return "!select,pselect6,_newselect,clock_gettime,sigaltstack,gettid,gettimeofday,nanosleep"
	}
}

// Export excluded syscalls as used by this package and multiple
// testsuites
var ExcludedSyscalls = getExcludedSyscalls()

func findStrace() (stracePath string, err error) {
	if path := filepath.Join(dirs.SnapMountDir, "strace-static", "current", "bin", "strace"); osutil.FileExists(path) {
		return path, nil
	}

	stracePath, err = exec.LookPath("strace")
	if err != nil {
		return "", fmt.Errorf("cannot find an installed strace, please try 'snap install strace-static'")
	}

	return stracePath, nil
}

// Command returns how to run strace in the users context with the right set of
// excluded system calls. The returned invocation of strace is wrapped with
// sudo.
func Command(extraStraceOpts []string) (*exec.Cmd, error) {
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("cannot use strace without sudo: %s", err)
	}

	// Try strace from the snap first, we use new syscalls like "_newselect"
	// that are known to not work with the strace of e.g. Ubuntu 14.04.
	//
	// TODO: some architectures do not have some syscalls (e.g. s390x does not
	// have _newselect). In https://github.com/strace/strace/issues/57 options
	// are discussed. We could use "-e trace=?syscall" but that is only
	// available since strace 4.17 which is not even in Ubuntu 17.10.
	stracePath, err := findStrace()
	if err != nil {
		return nil, fmt.Errorf("cannot find an installed strace, please try 'snap install strace-static'")
	}

	args := []string{
		sudoPath,
		"--",
		stracePath,
		"-f",
		"-e", ExcludedSyscalls,
	}
	args = append(args, extraStraceOpts...)

	return &exec.Cmd{
		Path: sudoPath,
		Args: args,
	}, nil
}

// CommandWithTraceePid returns strace invocation command with parameters for
// attaching to a specific process ID.
func CommandWithTraceePid(pid int, extraStraceOpts []string) (*exec.Cmd, error) {
	return Command(append(extraStraceOpts, "-p", strconv.Itoa(pid)))
}

// TraceExecCommandForPid returns an exec.Cmd suitable for attaching to a given
// process ID and tracking timings of execve{,at}() calls. Internally invokes
// strace wrapped with sudo.
func TraceExecCommandForPid(pid int, straceLogPath string) (*exec.Cmd, error) {
	extraStraceOpts := []string{
		"-ttt",                        // timestamps
		"-e", "trace=execve,execveat", // pick exec*() syscalls
		"-o", fmt.Sprintf("%s", straceLogPath), // output to FIFO
	}

	return CommandWithTraceePid(pid, extraStraceOpts)
}

func StraceAttachedStart(s string) bool {
	// we are expecting the tracing shim to issue a SIGSTOP notifying the parent
	// process it is ready to have strace attached to it, at which point the
	// parent will start strace, pass the child's pid and observe the output to
	// see an indication that strace attached to the child. In strace's output this looks like:
	//
	// strace: Process 1607676 attached
	// 1761133249.302069 --- stopped by SIGSTOP ---
	// 1761133255.979005 --- SIGCONT {si_signo=SIGCONT, si_code=SI_USER, si_pid=76275, si_uid=1000} ---
	//
	// where the timestamp's presence depends on the format parameters strace
	// was started with
	return strings.Contains(s, "--- stopped by SIGSTOP ---")
}
