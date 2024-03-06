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

package strace

import (
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"

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

func findStrace(u *user.User) (stracePath string, userOpts []string, err error) {
	if path := filepath.Join(dirs.SnapMountDir, "strace-static", "current", "bin", "strace"); osutil.FileExists(path) {
		// strace-static cannot resolve usernames, pass uid/gid instead
		return path, []string{"--uid", u.Uid, "--gid", u.Gid}, nil
	}

	stracePath, err = exec.LookPath("strace")
	if err != nil {
		return "", nil, fmt.Errorf("cannot find an installed strace, please try 'snap install strace-static'")
	}

	return stracePath, []string{"-u", u.Username}, nil
}

// Command returns how to run strace in the users context with the
// right set of excluded system calls.
func Command(extraStraceOpts []string, traceeCmd ...string) (*exec.Cmd, error) {
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("cannot use strace without sudo: %s", err)
	}

	// Try strace from the snap first, we use new syscalls like
	// "_newselect" that are known to not work with the strace of e.g.
	// ubuntu 14.04.
	//
	// TODO: some architectures do not have some syscalls (e.g.
	// s390x does not have _newselect). In
	// https://github.com/strace/strace/issues/57 options are
	// discussed.  We could use "-e trace=?syscall" but that is
	// only available since strace 4.17 which is not even in
	// ubutnu 17.10.
	stracePath, userOpts, err := findStrace(current)
	if err != nil {
		return nil, fmt.Errorf("cannot find an installed strace, please try 'snap install strace-static'")
	}

	args := []string{
		sudoPath,
		"-E",
		stracePath,
	}

	args = append(args, userOpts...)
	args = append(args, "-f", "-e", ExcludedSyscalls)
	args = append(args, extraStraceOpts...)
	args = append(args, traceeCmd...)

	return &exec.Cmd{
		Path: sudoPath,
		Args: args,
	}, nil
}

// TraceExecCommand returns an exec.Cmd suitable for tracking timings of
// execve{,at}() calls
func TraceExecCommand(straceLogPath string, origCmd ...string) (*exec.Cmd, error) {
	extraStraceOpts := []string{"-ttt", "-e", "trace=execve,execveat", "-o", fmt.Sprintf("%s", straceLogPath)}

	return Command(extraStraceOpts, origCmd...)
}
