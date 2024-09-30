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

	"github.com/snapcore/snapd/logger"
)

// KillSnapProcesses sends a signal to all the processes belonging to
// a given snap.
//
// A correct implementation is picked depending on cgroup v1 or v2 use in the
// system. For both cgroup v1 and v2, the call will act on all tracking groups
// of a snap.
//
// Note: Algorithms used for killing in cgroup v1 and v2 are slightly different.
//   - cgroup v1: freeze/kill/thaw.
//     This is to address multiple edge cases:
//     (1) Hybrid v1/v2 cgroups with pids controller mounted only on v1 or v2 (Ubuntu 20.04)
//     so we cannot guarantee having pids.max so we use the freezer cgroup instead.
//     (2) Address a known bug on systemd v237 for non-root users where transient scopes are
//     not created (e.g. on Ubuntu 18.04) so we use the freezer cgroup for tracking. This is
//     only useful for killing apps or processes which do not have their lifecycle managed by
//     external entities like systemd.
//   - cgroup v2: stop forking through pids.max, kill processes until cgroup is drained.
//     This is to address kernel versions without v2 freezer support so we use pids.max
//     to prevent fork bombs from racing with snapd.
var KillSnapProcesses = func(ctx context.Context, snapName string) error {
	return errors.New("KillSnapProcesses not implemented")
}

var syscallKill = syscall.Kill

var maxKillTimeout = 5 * time.Minute
var killThawCooldown = 100 * time.Millisecond

const killFreezeTimeout = 1 * time.Second

// killProcessesInCgroup sends SIGKILL signal to all the processes belonging to
// passed cgroup directory.
//
// The caller is responsible for making sure that pids are not reused
// after reading `cgroup.procs` to avoid TOCTOU.
//
// The freeze() callback is called exactly before killing pids is started while the thaw()
// callback is called exactly after killing pids ends and before returning errors to give
// a chance to recover a cgroup from a frozen state. Not propagating errors from the
// callbacks is intentional to make it clear that they are best effort.
var killProcessesInCgroup = func(ctx context.Context, dir string, freeze func(ctx context.Context), thaw func()) error {
	// Keep sending SIGKILL signals until no more pids are left in cgroup
	// to cover the case where a process forks before we kill it.
	for {
		pids, err := pidsInFile(filepath.Join(dir, "cgroup.procs"))
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			// no more pids
			return nil
		}

		if freeze != nil {
			freeze(ctx)
		}
		var firstErr error
		for _, pid := range pids {
			// This prevents a rogue fork bomb from keeping this loop running forever
			select {
			case <-ctx.Done():
				return fmt.Errorf("cannot kill processes in cgroup %q: %w", dir, ctx.Err())
			default:
			}

			pidNotFoundErr := syscall.ESRCH
			// TODO: Use pidfs when possible to avoid killing reused pids.
			if err := syscallKill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, pidNotFoundErr) && firstErr == nil {
				firstErr = err
			}
		}
		if thaw != nil {
			// thaw() must be called before returning to avoid keeping cgroup stuck after freeze()
			thaw()
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
	ctxWithTimeout, cancel := context.WithTimeout(ctx, maxKillTimeout)
	defer cancel()

	freeze := func(ctx context.Context) {
		// This is best effort freezing, ignore all errors and continue with
		// processes killing.
		// This accounts for two scenarios:
		//   - Classic snaps without a freezer cgroup
		//   - A bug in some kernel versions where sometimes a cgroup get stuck
		//     in FREEZING state. Given that maxKillTimeout is bigger than timeout passed to freezer
		//     This gives a chance to thaw the cgroup and trying again.
		ctxWithTimeout, cancel := context.WithTimeout(ctx, killFreezeTimeout)
		defer cancel()
		err := freezeSnapProcessesImplV1(ctxWithTimeout, snapName)
		if err != nil && !isCgroupNotExistErr(err) {
			logger.Noticef("could not freeze cgroup while killing %q processes: %v", snapName, err)
		}
	}
	thaw := func() {
		// SIGKILL on a frozen cgroup will not take effect until the cgroup is thawed
		thawSnapProcessesImplV1(snapName)
		// Give for the sent SIGKILL signals to take effect on the next loop
		time.Sleep(killThawCooldown)
	}
	killCgroupProcs := func(dir string) error {
		return killProcessesInCgroup(ctxWithTimeout, dir, freeze, thaw)
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

	// This is a workaround for systemd v237 (used by Ubuntu 18.04) for non-root users
	// where a transient scope cgroup is not created for a snap hence it cannot be tracked
	// by the usual snap.<security-tag>-<uuid>.scope pattern.
	// Here, We rely on the fact that snap-confine moves the snap pids into the freezer cgroup
	// created for the snap.
	err := killProcessesInCgroup(ctxWithTimeout, filepath.Join(freezerCgroupV1Dir, fmt.Sprintf("snap.%s", snapName)), freeze, thaw)
	if err != nil && !isCgroupNotExistErr(err) && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

func killSnapProcessesImplV2(ctx context.Context, snapName string) error {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, maxKillTimeout)
	defer cancel()

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

		if err := killProcessesInCgroup(ctxWithTimeout, dir, nil, nil); err != nil {
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
