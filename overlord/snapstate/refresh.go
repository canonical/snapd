// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package snapstate

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

// SoftNothingRunningRefreshCheck looks if there are at most only service processes alive.
//
// The check is designed to run early in the refresh pipeline. Before
// downloading or stopping services for the update, we can check that only
// services are running, that is, that no non-service apps or hooks are
// currently running.
//
// Since services are stopped during the update this provides a good early
// precondition check.  The check is also deliberately racy, both at the level
// of processes forking and exiting and at the level of snap-confine launching
// new commands. The hard check needs to be synchronized but the soft check
// doesn't require this since it would serve no purpose. After the soft check
// passes the user is free to start snap applications and block the hard check.
func SoftNothingRunningRefreshCheck(info *snap.Info) error {
	// Find the set of PIDs that belong to the snap, excluding any that belong
	// to services since services are stopped for refresh and will likely not
	// interfere.
	snapName := info.SnapName()
	pidSet, err := pidSetOfSnap(snapName)
	if err != nil {
		return err
	}
	for _, app := range info.Apps {
		if app.IsService() {
			pids, err := pidsOfSecurityTag(app.SecurityTag())
			if err != nil {
				return err
			}
			for _, pid := range pids {
				delete(pidSet, pid)
			}
		}
	}
	if len(pidSet) == 0 {
		return nil
	}

	// Some PIDs belong to non-service applications. Let's find out what we
	// can and produce an informative error.
	pids := make([]int, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	sort.Ints(pids)

	var busyAppNames []string
	for name, app := range info.Apps {
		// Skip services since those are filtered out above.
		if app.IsService() {
			continue
		}
		isBusy, err := isSecurityTagBusy(app.SecurityTag())
		if err != nil {
			return err
		}
		if isBusy {
			busyAppNames = append(busyAppNames, name)
		}
	}
	sort.Strings(busyAppNames)

	var busyHookNames []string
	for name, hook := range info.Hooks {
		isBusy, err := isSecurityTagBusy(hook.SecurityTag())
		if err != nil {
			return err
		}
		if isBusy {
			busyHookNames = append(busyHookNames, name)
		}
	}
	sort.Strings(busyHookNames)

	return &BusySnapError{
		pids:          pids,
		snapName:      snapName,
		busyAppNames:  busyAppNames,
		busyHookNames: busyHookNames,
	}
}

// HardNothingRunningRefreshCheck looks if there are any processes alive.
//
// The check is designed to run late in the refresh pipeline, after stopping
// snap services. At this point services should be stopped, hooks should no
// longer run, and applications should be barred from running externally (e.g.
// by grabbing the per-snap lock around that phase of the update).
//
// The check looks at the set of PIDs in the freezer cgroup associated with a
// given snap. Presence of any processes indicates that a snap is busy and
// refresh cannot proceed.
func HardNothingRunningRefreshCheck(snapName string) error {
	pidSet, err := pidSetOfSnap(snapName)
	if err != nil {
		return err
	}
	if len(pidSet) > 0 {
		pids := make([]int, 0, len(pidSet))
		for pid := range pidSet {
			pids = append(pids, pid)
		}
		sort.Ints(pids)
		return &BusySnapError{pids: pids, snapName: snapName}
	}
	return nil
}

// BusySnapError indicates that snap has apps or hooks running and cannot refresh.
type BusySnapError struct {
	snapName      string
	pids          []int
	busyAppNames  []string
	busyHookNames []string
}

// Error formats an error string describing what is running.
func (err *BusySnapError) Error() string {
	switch {
	case len(err.busyAppNames) > 0 && len(err.busyHookNames) > 0:
		return fmt.Sprintf("snap %q has running apps (%s) and hooks (%s)",
			err.snapName, strings.Join(err.busyAppNames, ", "), strings.Join(err.busyHookNames, ", "))
	case len(err.busyAppNames) > 0:
		return fmt.Sprintf("snap %q has running apps (%s)",
			err.snapName, strings.Join(err.busyAppNames, ", "))
	case len(err.busyHookNames) > 0:
		return fmt.Sprintf("snap %q has running hooks (%s)",
			err.snapName, strings.Join(err.busyHookNames, ", "))
	default:
		return fmt.Sprintf("snap %q has running apps or hooks", err.snapName)
	}
}

// Pids returns the set of process identifiers that are running.
//
// Since this list is a snapshot it should be only acted upon if there is an
// external synchronization system applied (e.g. all processes are frozen) at
// the time the snapshot was taken.
//
// The list is intended for snapd to forcefully kill all processes for a forced
// refresh scenario.
func (err BusySnapError) Pids() []int {
	return err.pids
}

// isSecurityTagBusy returns true if there are any processes belonging to a given security tag.
func isSecurityTagBusy(securityTag string) (bool, error) {
	pids, err := pidsOfSecurityTag(securityTag)
	return len(pids) > 0, err
}

// parsePid parses a string as a process identifier.
func parsePid(text string) (int, error) {
	pid, err := strconv.Atoi(text)
	if err == nil && pid <= 0 {
		return 0, fmt.Errorf("cannot parse pid %q", text)
	}
	return pid, err
}

// parsePids parses a list of pids, one per line, from a reader.
func parsePids(reader io.Reader) ([]int, error) {
	scanner := bufio.NewScanner(reader)
	var pids []int
	for scanner.Scan() {
		s := scanner.Text()
		pid, err := parsePid(s)
		if err != nil {
			return nil, err
		}
		pids = append(pids, pid)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pids, nil
}

// pidSetOfSnap returns a set of PIDs belonging to a given snap.
//
// The set is obtained from a freezer cgroup. It is designed for ease of
// modification by the caller.
func pidSetOfSnap(snapName string) (map[int]bool, error) {
	fname := filepath.Join(dirs.FreezerCgroupDir, "snap."+snapName, "cgroup.procs")
	file, err := os.Open(fname)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	pids, err := parsePids(bufio.NewReader(file))
	if err != nil {
		return nil, err
	}

	pidSet := make(map[int]bool, len(pids))
	for _, pid := range pids {
		pidSet[pid] = true
	}
	return pidSet, nil
}

// pidsOfSecurityTag returns a list of PIDs belonging to a given security tag.
//
// The list is obtained from a pids cgroup.
func pidsOfSecurityTag(securityTag string) ([]int, error) {
	fname := filepath.Join(dirs.PidsCgroupDir, securityTag, "cgroup.procs")
	file, err := os.Open(fname)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parsePids(bufio.NewReader(file))
}
