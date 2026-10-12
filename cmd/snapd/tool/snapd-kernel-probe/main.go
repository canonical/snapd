// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// Package snapd_kernel_probe implements the snapd-kernel-probe command entry
// point. It is used by cmd/snapd when invoked via the C tool wrapper with
// argv[1]="snapd-kernel-probe".
//
// snapd-kernel-probe runs small probes against the running kernel to detect
// specific bugs snapd needs to work around at runtime. Each probe is a
// subcommand. Probes that need to run under an AppArmor profile are confined
// via the same attr/exec mechanism aa-exec uses: the tool asks for the profile
// to be applied at the next exec and re-executes itself with an internal
// "-confined" subcommand which performs the actual exercise.
package snapd_kernel_probe

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"
)

// For mocking in tests.
var (
	osWriteFile = os.WriteFile
	syscallExec = syscall.Exec
)

// procThreadSelfAttrExec is the procfs file controlling the AppArmor profile
// transition applied at the next exec(2) of the calling thread.
var procThreadSelfAttrExec = "/proc/thread-self/attr/exec"

const (
	networkBugProbe         = "apparmor-5-network-bug"
	networkBugConfinedProbe = "apparmor-5-network-bug-confined"

	// exitInternalError is reported when the probe itself could not run, as
	// opposed to the probed operation failing with an errno.
	exitInternalError = 125

	probeTimeout = 5 * time.Second
)

// selfExe is re-executed to enter the confined probe. Executing the running
// binary directly (rather than through snapd-tool-wrap) keeps the profile
// transition on the very exec that starts the probe.
const selfExe = "/proc/self/exe"

// runConfinedSelf transitions into the given AppArmor profile at exec time
// (the exact mechanism aa-exec uses: write "exec <profile>" to
// /proc/self/attr/exec, then exec) and replaces this process with the
// confined probe. The probe's exit status becomes ours.
func runConfinedSelf(profileName string) error {
	// The probe runs in a private network namespace (the caller starts us
	// with CLONE_NEWNET) whose loopback is down. Raise it now, while still
	// unconfined, so the confined probe needs no capability or ioctl
	// permissions and can run before the host's loopback is up.
	if err := raiseLoopback(); err != nil {
		return err
	}

	// Transition at exec: the kernel re-evaluates confinement on exec(2)
	// and, because the probe profile has no attachment, an immediate
	// change_profile via attr/current would be lost across the exec.
	// Writing "exec <profile>" to attr/exec makes the transition happen
	// *at* the exec, exactly like aa-exec -p.
	//
	// The pending exec label is thread-local kernel state, so pin this
	// goroutine to its OS thread to keep the runtime from migrating it
	// between the write and the exec, and use thread-self so the write
	// targets this thread rather than the process leader (/proc/self
	// resolves via the group leader, not the calling thread). The same
	// locking pattern is used for per-thread kernel state in
	// osutil/sys/runas.go. The thread is deliberately left locked: the
	// exec either succeeds and replaces the thread image, or this
	// dedicated probe process exits with an error right away.
	runtime.LockOSThread()
	if err := osWriteFile(procThreadSelfAttrExec, []byte("exec "+profileName), 0600); err != nil {
		return fmt.Errorf("cannot set AppArmor exec profile %q: %v", profileName, err)
	}

	// argv[0] is "snapd" so that cmd/snapd dispatches to the tool.
	argv := []string{"snapd", "snapd-kernel-probe", networkBugConfinedProbe}
	return syscallExec(selfExe, argv, os.Environ())
}

// probeFailure describes the first operation of the exercise that failed.
type probeFailure struct {
	op  string
	err error
}

// reportFailure prints the failure in the form consumed by
// sandbox/apparmor and returns the process exit status: the errno of the
// failed operation, or exitInternalError if there is none.
func reportFailure(w io.Writer, f *probeFailure) int {
	var errno syscall.Errno
	if errors.As(f.err, &errno) && errno > 0 && errno < exitInternalError {
		fmt.Fprintf(w, "FAIL %s %d\n", f.op, int(errno))
		return int(errno)
	}
	fmt.Fprintf(w, "error: cannot %s: %v\n", f.op, f.err)
	return exitInternalError
}

// exerciseLoopback exchanges a message over a TCP connection on the IPv4
// loopback within a single process. The plain read and write on the connected
// socket are the operations mis-mediated by the AppArmor 5.0 bug. It returns
// nil if everything worked.
func exerciseLoopback() *probeFailure {
	deadline := time.Now().Add(probeTimeout)

	// Explicit tcp4 so the Go runtime does not probe for IPv6 support,
	// which the profile does not allow.
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return &probeFailure{"listen", err}
	}
	defer l.Close()
	l.(*net.TCPListener).SetDeadline(deadline)

	server := make(chan *probeFailure, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			server <- &probeFailure{"accept", err}
			return
		}
		defer conn.Close()
		conn.SetDeadline(deadline)
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			server <- &probeFailure{"read", err}
			return
		}
		if _, err := conn.Write(buf); err != nil {
			server <- &probeFailure{"write", err}
			return
		}
		server <- nil
	}()

	conn, err := net.DialTimeout("tcp4", l.Addr().String(), probeTimeout)
	if err != nil {
		return &probeFailure{"connect", err}
	}
	defer conn.Close()
	conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte("ping")); err != nil {
		return &probeFailure{"write", err}
	}
	if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
		return &probeFailure{"read", err}
	}
	return <-server
}

// runConfined is the confined half of the network bug probe. It returns the
// process exit status.
func runConfined(w io.Writer) int {
	if f := exerciseLoopback(); f != nil {
		return reportFailure(w, f)
	}
	return 0
}

// run executes snapd-kernel-probe with the given args. For the confined
// probe it returns the exit status to use; for others a non-nil error means
// exit status 1.
func run(args []string) (int, error) {
	var probe, profileName string
	switch len(args) {
	case 1:
		probe = args[0]
	case 2:
		probe, profileName = args[0], args[1]
	default:
		return 1, fmt.Errorf("usage: snapd-kernel-probe <probe> [profile]")
	}

	switch probe {
	case networkBugProbe:
		if profileName == "" {
			return 1, fmt.Errorf("probe %q requires an AppArmor profile name", probe)
		}
		return 1, runConfinedSelf(profileName)
	case networkBugConfinedProbe:
		return runConfined(os.Stderr), nil
	default:
		return 1, fmt.Errorf("unknown probe %q", probe)
	}
}

// Main is the entry point for snapd-kernel-probe. It exits the process on
// error; on success of the unconfined entry the process is replaced by the
// confined probe and its exit status is what the caller observes.
func Main() {
	status, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	os.Exit(status)
}
