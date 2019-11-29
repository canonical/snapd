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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
)

func securityTagFromCgroupPath(path string) (securityTag string) {
	leaf := filepath.Base(path)
	if matched, _ := filepath.Match("snap.*.service", leaf); matched {
		return strings.TrimSuffix(leaf, ".service")
	}
	if matched, _ := filepath.Match("snap.*.scope", leaf); matched {
		// Neither dot1 nor dor2 can be -1 because the match guarantees that at
		// least two dots exist.
		dot1 := strings.IndexRune(leaf, '.')
		dot2 := strings.IndexRune(leaf[dot1+1:], '.')
		return strings.TrimSuffix(leaf[:dot1]+leaf[dot1+dot2+1:], ".scope")
	}
	return ""
}

// pidsOfSnap returns the association of security tags to PIDs.
//
// NOTE: This function returns non-empty result only if refresh-app-awareness
// is enabled.
//
// The return value is a snapshot of the pids of a given snap, grouped by
// security tag. The result may be immediately stale as processes fork and
// exit but it has the following guarantee.
//
// If the per-snap lock is held while computing the set, then the following
// guarantee is true: If a security tag is not among the result then no such
// tag can come into existence while the lock is held.
//
// This can be used to classify the activity of a given snap into activity
// classes, based on the nature of the security tags encountered.
//
// TODO: move this to sandbox/cgroup later.
func pidsOfSnap(snapInfo *snap.Info) (map[string][]int, error) {
	// pidsByTag maps security tag to a list of pids.
	pidsByTag := make(map[string][]int, len(snapInfo.Apps)+len(snapInfo.Hooks))
	securityTagPrefix := "snap." + snapInfo.InstanceName() + "."

	// Walk the cgroup tree and look for "cgroup.procs" files. Having found one
	// we try to derive the snap security tag from one. If successful and the
	// tag matches the snap we are interested in we havrvest the snapshot of
	// PIDs that belong to the cgroup and bin them into a bucket associated
	// with the security tag.
	walkFunc := func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil || fileInfo.IsDir() {
			return err
		}
		if filepath.Base(path) != "cgroup.procs" {
			return nil
		}
		cgroupPath := filepath.Dir(path)
		cgroupPath = filepath.Clean(cgroupPath) // Drops trailing /
		securityTag := securityTagFromCgroupPath(cgroupPath)
		if securityTag == "" {
			return nil
		}
		if !strings.HasPrefix(securityTag, securityTagPrefix) {
			return nil
		}
		pids, err := cgroup.PidsInFile(path)
		if err != nil {
			return err
		}
		pidsByTag[securityTag] = append(pidsByTag[securityTag], pids...)
		return nil
	}

	// TODO: Currently we walk the entire cgroup tree. We could be more precise
	// if we knew which of the fundamental two modes are used.
	//
	// In v2 mode, when /sys/fs/cgroup is a cgroup2 mount then the code is
	// correct as-is.  In v1 mode, either with hybrid or without, we could walk
	// a more scoped subset, specifically /sys/fs/cgroup/unified in hybrid
	// mode, if one exists, or /sys/fs/cgroup/systemd as last-resort fallback.
	//
	// NOTE: Walk is internally performed in lexical order so the output is
	// deterministic and we don't need to sort the returned aggregated PIDs.
	if err := filepath.Walk(dirs.CgroupDir, walkFunc); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return pidsByTag, nil

}

func genericRefreshCheck(info *snap.Info, canAppRunDuringRefresh func(app *snap.AppInfo) bool) error {
	// Grab per-snap lock to prevent new processes from starting. This is
	// sufficient to perform the check, even though individual processes
	// may fork or exit, we will have per-security-tag information about
	// what is running.
	lock, err := snaplock.OpenLock(info.SnapName())
	if err != nil {
		return err
	}
	// Closing the lock also unlocks it, if locked.
	defer lock.Close()
	if err := lock.Lock(); err != nil {
		return err
	}
	knownPids, err := pidsOfSnap(info)
	if err != nil {
		return err
	}
	lock.Unlock()

	var busyAppNames []string
	var busyHookNames []string
	var busyPIDs []int

	// Currently there are no situations when hooks might be allowed to run
	// during the refresh process. The function exists to make the next two
	// chunks of code symmetric.
	canHookRunDuringRefresh := func(hook *snap.HookInfo) bool {
		return false
	}

	for name, app := range info.Apps {
		if canAppRunDuringRefresh(app) {
			continue
		}
		if PIDs := knownPids[app.SecurityTag()]; len(PIDs) > 0 {
			busyAppNames = append(busyAppNames, name)
			busyPIDs = append(busyPIDs, PIDs...)
		}
	}

	for name, hook := range info.Hooks {
		if canHookRunDuringRefresh(hook) {
			continue
		}
		if PIDs := knownPids[hook.SecurityTag()]; len(PIDs) > 0 {
			busyHookNames = append(busyHookNames, name)
			busyPIDs = append(busyPIDs, PIDs...)
		}
	}
	if len(busyAppNames) == 0 && len(busyHookNames) == 0 {
		return nil
	}
	sort.Strings(busyAppNames)
	sort.Strings(busyHookNames)
	sort.Ints(busyPIDs)
	return &BusySnapError{
		snapName:      info.SnapName(),
		busyAppNames:  busyAppNames,
		busyHookNames: busyHookNames,
		pids:          busyPIDs,
	}
}

// SoftNothingRunningRefreshCheck looks if there are at most only service processes alive.
//
// The check is designed to run early in the refresh pipeline. Before
// downloading or stopping services for the update, we can check that only
// services are running, that is, that no non-service apps or hooks are
// currently running.
//
// Since services are stopped during the update this provides a good early
// precondition check. The check is also deliberately racy as existing snap
// commands can fork new processes or existing processes can die. After the
// soft check passes the user is free to start snap applications and block the
// hard check.
func SoftNothingRunningRefreshCheck(info *snap.Info) error {
	return genericRefreshCheck(info, func(app *snap.AppInfo) bool {
		return app.IsService()
	})
}

// HardNothingRunningRefreshCheck looks if there are any undesired processes alive.
//
// The check is designed to run late in the refresh pipeline, after stopping
// snap services. At this point non-enduring services should be stopped, hooks
// should no longer run, and applications should be barred from running
// externally (e.g. by using a new inhibition mechanism for snap run).
//
// The check fails if any process belonging to the snap, apart from services
// that are enduring refresh, is still alive. If a snap is busy it cannot be
// refreshed and the refresh process is aborted.
func HardNothingRunningRefreshCheck(info *snap.Info) error {
	return genericRefreshCheck(info, func(app *snap.AppInfo) bool {
		// TODO: use a constant instead of "endure"
		return app.IsService() && app.RefreshMode == "endure"
	})
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
