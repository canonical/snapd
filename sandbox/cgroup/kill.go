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
	"time"
)

// KillSnapProcesses sends a signal to all the processes belonging to
// a given snap.
//
// A correct implementation is picked depending on cgroup v1 or v2 use in the
// system. When cgroup v1 is detected, the call will directly act on the freezer
// group created when a snap process was started, while with v2 the call will
// act on all tracking groups of a snap.
//
// Note: Algorithms used for killing in cgroup v1 and v2 are different.
//   - cgroup v1: freeze/kill/thaw.
//     This is to address multiple edge cases:
//     (1) Hybrid v1/v2 cgroups with pids controller mounted only on v1 or v2 (Ubuntu 20.04).
//     (2) Address a known bug on systemd v327 for non-root users where transient scopes are not created (Ubuntu 18.04).
//   - cgroup v2: stop forking, kill processes until cgroup is drained.
//     This is to address kernel versions without v2 freezer support.
var KillSnapProcesses = func(ctx context.Context, snapName string) error {
	return errors.New("KillSnapProcesses not implemented")
}

var syscallKill = syscall.Kill

var maxKillTimeout = 1 * time.Minute

// killProcessesInCgroup sends signal to all the processes belonging to
// passed cgroup directory.
//
// The caller is responsible for making sure that pids are not reused
// after reading `cgroup.procs` to avoid TOCTOU.
var killProcessesInCgroup = func(ctx context.Context, dir string) error {
	// Keep sending SIGKILL signals until no more pids are left in cgroup
	// to cover the case where a process forks before we kill it.
	ctxWithTimeout, cancel := context.WithTimeout(ctx, maxKillTimeout)
	defer cancel()
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

		// This prevents a rouge fork bomb from keeping this loop running forever
		select {
		case <-ctxWithTimeout.Done():
			return fmt.Errorf("cannot kill processes in cgroup %q: %w", dir, ctxWithTimeout.Err())
		default:
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

func isCgroupNotExistErr(err error) bool {
	// fs.ErrNotExist and ENODEV are ignored in case the cgroup went away while we were
	// processing the cgroup. ENODEV is returned by the kernel if the cgroup went
	// away while a kernfs operation is ongoing.
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENODEV)
}

func killSnapProcessesImplV1(ctx context.Context, snapName string) error {
	err := freezeSnapProcessesImplV1(ctx, snapName)
	if err != nil && !isCgroupNotExistErr(err) {
		return err
	}
	// For V1, SIGKILL on a frozen cgroup will not take effect
	// until the cgroup is thawed
	defer thawSnapProcessesImplV1(snapName)

	err = killProcessesInCgroup(ctx, filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName)))
	if err != nil && !isCgroupNotExistErr(err) {
		return err
	}

	return nil
}

func killSnapProcessesImplV2(ctx context.Context, snapName string) error {
	killCgroupProcs := func(dir string) error {
		// Use cgroup.kill if it exists (requires linux 5.14+)
		err := writeExistingFile(filepath.Join(dir, "cgroup.kill"), []byte("1"))
		if err == nil || !isCgroupNotExistErr(err) {
			return err
		}

		// Fallback to killing each pid if cgroup.kill doesn't exist

		// Set pids.max to 0 to prevent a fork bomb from racing with us.
		err = writeExistingFile(filepath.Join(dir, "pids.max"), []byte("0"))
		// Let's continue to killing pids if the pids.max doesn't exist because
		// it could be the case on hybrid systems that the pids controller is
		// mounted for v1 cgroups and not available in v2 so let's give snapd
		// a chance to kill this process even if we can't limit its number of
		// processes (hoping we win against a fork bomb).
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		if err := killProcessesInCgroup(ctx, dir); err != nil {
			return err
		}
		return nil
	}

	var firstErr error
	skipError := func(err error) bool {
		if !isCgroupNotExistErr(err) && firstErr == nil {
			firstErr = err
		}
		return true
	}

	if err := applyToSnap(snapName, killCgroupProcs, skipError); err != nil {
		return err
	}

	return firstErr
}
