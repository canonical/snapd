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
// system. For both cgroup v1 and v2, the call will act on all tracking groups
// of a snap.
//
// Note: When cgroup v1 is detected, the call will also act on the freezer
// group created when a snap process was started to address a known bug on
// systemd v327 for non-root users.
var KillSnapProcesses = func(ctx context.Context, snapName string) error {
	return errors.New("KillSnapProcesses not implemented")
}

var syscallKill = syscall.Kill

// killProcessesInCgroup sends signal to all the processes belonging to
// passed cgroup directory.
//
// The caller is responsible for making sure that pids are not reused
// after reading `cgroup.procs` to avoid TOCTOU.
var killProcessesInCgroup = func(dir string) error {
	// Keep sending SIGKILL signals until no more pids are left in cgroup
	// to cover the case where a process forks before we kill it.
	for {
		// XXX: Should this have maximum retries?
		pids, err := pidsInFile(filepath.Join(dir, "cgroup.procs"))
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			// no more pids
			return nil
		}

		var firstErr error
		for _, pid := range pids {
			pidNotFoundErr := syscall.ESRCH
			// TODO: Use pidfs when possible to avoid killing reused pids.
			if err := syscallKill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, pidNotFoundErr) && firstErr == nil {
				firstErr = err
			}
		}
		if firstErr != nil {
			return firstErr
		}
	}
}

func killSnapProcessesImplV1(ctx context.Context, snapName string) error {
	var firstErr error
	skipError := func(err error) bool {
		// fs.ErrNotExist and ENODEV are ignored in case the cgroup went away while we were
		// processing the cgorup. ENODEV is returned by the kernel if the cgroup went
		// away while a kernfs operation is ongoing.
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENODEV) && firstErr == nil {
			firstErr = err
		}
		return true
	}

	if err := applyToSnap(snapName, killProcessesInCgroup, skipError); err != nil {
		return err
	}

	// This is a workaround for systemd v237 (used by Ubuntu 18.04) for non-root users
	// where a transient scope cgroup is not created for a snap hence it cannot be tracked
	// by the usual snap.<security-tag>-<uuid>.scope pattern.
	// Here, We rely on the fact that snap-confine moves the snap pids into the freezer cgroup
	// created for the snap.
	// There is still a tiny race window between "snap run" unlocking the run inhibition lock
	// and snap-confine moving pids to the freezer cgroup where we would miss those pids.
	err := killProcessesInCgroup(filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName)))
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENODEV) && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func killSnapProcessesImplV2(ctx context.Context, snapName string) error {
	killCgroupProcs := func(dir string) error {
		// Use cgroup.kill if it exists (requires linux 5.14+)
		err := writeExistingFile(filepath.Join(dir, "cgroup.kill"), []byte("1"))
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		// Fallback to killing each pid if cgroup.kill doesn't exist
		if err := killProcessesInCgroup(dir); err != nil {
			return err
		}
		return nil
	}

	var firstErr error
	skipError := func(err error) bool {
		// fs.ErrNotExist and ENODEV are ignored in case the cgroup went away while we were
		// processing the cgorup. ENODEV is returned by the kernel if the cgroup went
		// away while a kernfs operation is ongoing.
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENODEV) && firstErr == nil {
			firstErr = err
		}
		return true
	}

	if err := applyToSnap(snapName, killCgroupProcs, skipError); err != nil {
		return err
	}

	return firstErr
}
