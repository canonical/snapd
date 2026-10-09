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
// via the same attr/exec mechanism aa-exec uses, then exec the static helper
// binary (snapd-kernel-probe-helper) which performs the raw syscall exercise.
package snapd_kernel_probe

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

// For mocking in tests.
var (
	osWriteFile            = os.WriteFile
	syscallExec            = syscall.Exec
	snapdKernelProbeHelper = "snapd-kernel-probe-helper"
)

// procThreadSelfAttrExec is the procfs file controlling the AppArmor profile
// transition applied at the next exec(2) of the calling thread.
var procThreadSelfAttrExec = "/proc/thread-self/attr/exec"

// runConfinedHelper transitions into the given AppArmor profile at exec time
// (the exact mechanism aa-exec uses: write "exec <profile>" to
// /proc/self/attr/exec, then exec) and replaces this process with the static
// probe helper binary. The helper's exit status becomes ours.
func runConfinedHelper(profileName string) error {
	helperPath, err := snapdtool.InternalToolPath(snapdKernelProbeHelper)
	if err != nil {
		return fmt.Errorf("cannot locate %s: %v", snapdKernelProbeHelper, err)
	}
	if !osutil.IsExecutable(helperPath) {
		return fmt.Errorf("%s is not executable: %s", snapdKernelProbeHelper, helperPath)
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

	return syscallExec(helperPath, []string{filepath.Base(helperPath)}, os.Environ())
}

// run executes snapd-kernel-probe with the given args.
func run(args []string) error {
	var probe, profileName string
	switch len(args) {
	case 1:
		probe = args[0]
	case 2:
		probe, profileName = args[0], args[1]
	default:
		return fmt.Errorf("usage: snapd-kernel-probe <probe> [profile]")
	}

	switch probe {
	case "apparmor-5-network-bug":
		if profileName == "" {
			return fmt.Errorf("probe %q requires an AppArmor profile name", probe)
		}
		return runConfinedHelper(profileName)
	default:
		return fmt.Errorf("unknown probe %q", probe)
	}
}

// Main is the entry point for snapd-kernel-probe. It exits the process on
// error; on success the process is replaced by the probe helper and the
// helper's exit status is what the caller observes.
func Main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
