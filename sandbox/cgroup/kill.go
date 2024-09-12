// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package cgroup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
)

// KillSnapProcesses sends a signal to all the processes belonging to
// a given snap.
//
// A correct implementation is picked depending on cgroup v1 or v2 use in the
// system. When cgroup v1 is detected, the call will directly act on the freezer
// group created when a snap process was started, while with v2 the call will
// act on all tracking groups of a snap.
//
// XXX: It is important to note that the implementation for v2 is racy because
// processes can be killed externally even when the cgroup is frozen and have
// their pid reused. Also, v2 freezing support is a no-op on kernels before
// 5.2 which means new processes can keep getting spawned while killing
// pids read earlier.
var KillSnapProcesses = func(ctx context.Context, snapName string) error {
	return errors.New("KillSnapProcesses not implemented")
}

var syscallKill = syscall.Kill

// killProcessesInCgroup sends signal to all the processes belonging to
// passed cgroup directory.
//
// The caller is responsible for making sure that pids are not reused
// after reading `cgroup.procs` to avoid TOCTOU.
func killProcessesInCgroup(dir string, signal syscall.Signal) error {
	pids, err := pidsInFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return err
	}

	var firstErr error
	for _, pid := range pids {
		pidNotFoundErr := syscall.ESRCH
		if err := syscallKill(pid, signal); err != nil && !errors.As(err, &pidNotFoundErr) && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func killSnapProcessesImplV1(ctx context.Context, snapName string) error {
	if err := freezeSnapProcessesImplV1(ctx, snapName); err != nil {
		return err
	}
	// For V1, SIGKILL on a frozen cgroup will not take effect
	// until the cgroup is thawed
	defer thawSnapProcessesImplV1(snapName)

	return killProcessesInCgroup(filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName)), syscall.SIGKILL)
}

// XXX: killSnapProcessesImplV2 is racy to varying degrees depending on the kernel
// version.
//
//  1. Cgroup v2 freezer was only available since Linux 5.2 so freezing is a no-op before 5.2 which allows processes to keep forking.
//  2. Freezing does not put processes in an uninterruptable sleep unlike v1, so they can be killed externally and have their pid reused.
//  3. `cgroup.kill` was introduced in Linux 5.14 and solves the above issues as it kills the cgroup processes atomically.
func killSnapProcessesImplV2(ctx context.Context, snapName string) error {
	killCgroupProcs := func(dir string) error {
		// Use cgroup.kill if it exists (requires linux 5.14+)
		err := writeExistingFile(filepath.Join(dir, "cgroup.kill"), []byte("1"))
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// Fallback to classic freeze/kill/thaw if cgroup.kill doesn't exist

		if err := freezeOneV2(ctx, dir); err != nil {
			return err
		}
		if err := killProcessesInCgroup(dir, syscall.SIGKILL); err != nil {
			// Thaw on error to avoid keeping cgroup stuck
			thawOneV2(dir) // ignore the error, this is best-effort
			return err
		}
		return nil
	}

	var firstErr error
	skipError := func(err error) bool {
		if !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
		return true
	}

	if err := applyToSnap(snapName, killCgroupProcs, skipError); err != nil {
		return err
	}

	return firstErr
}
