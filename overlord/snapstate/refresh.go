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
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	userclient "github.com/snapcore/snapd/usersession/client"
)

// pidsOfSnap is a mockable version of PidsOfSnap
var pidsOfSnap = cgroup.PidsOfSnap

// refreshAppsCheck returns an error if the snap has processes running that aren't
// services and aren't marked to be ignored (refresh-mode: "ignore-running").
var refreshAppsCheck = func(info *snap.Info) error {
	knownPids, err := pidsOfSnap(info.InstanceName())
	if err != nil {
		return err
	}

	// Due to specific of the interaction with locking, all locking is performed by the caller.
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
		if app.IsService() || app.RefreshMode == "ignore-running" {
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
		SnapInfo:      info,
		busyAppNames:  busyAppNames,
		busyHookNames: busyHookNames,
		pids:          busyPIDs,
	}
}

// BusySnapError indicates that snap has apps or hooks running and cannot refresh.
type BusySnapError struct {
	SnapInfo      *snap.Info
	pids          []int
	busyAppNames  []string
	busyHookNames []string
}

// PendingSnapRefreshInfo computes information necessary to perform user notification
// of postponed refresh of a snap, based on the information about snap "business".
//
// The returned value contains the instance name of the snap as well as, if possible,
// information relevant for desktop notification services, such as application name
// and the snapd-generated desktop file name.
func (err *BusySnapError) PendingSnapRefreshInfo() *userclient.PendingSnapRefreshInfo {
	refreshInfo := &userclient.PendingSnapRefreshInfo{
		InstanceName: err.SnapInfo.InstanceName(),
	}
	for _, appName := range err.busyAppNames {
		if app, ok := err.SnapInfo.Apps[appName]; ok {
			path := app.DesktopFile()
			if osutil.FileExists(path) {
				refreshInfo.BusyAppName = appName
				refreshInfo.BusyAppDesktopEntry = strings.SplitN(filepath.Base(path), ".", 2)[0]
				break
			}
		}
	}
	return refreshInfo
}

// Error formats an error string describing what is running.
func (err *BusySnapError) Error() string {
	pids := strutil.IntsToCommaSeparated(err.pids)
	switch {
	case len(err.busyAppNames) > 0 && len(err.busyHookNames) > 0:
		return fmt.Sprintf("snap %q has running apps (%s) and hooks (%s), pids: %s",
			err.SnapInfo.InstanceName(), strings.Join(err.busyAppNames, ", "), strings.Join(err.busyHookNames, ", "), pids)
	case len(err.busyAppNames) > 0:
		return fmt.Sprintf("snap %q has running apps (%s), pids: %s",
			err.SnapInfo.InstanceName(), strings.Join(err.busyAppNames, ", "), pids)
	case len(err.busyHookNames) > 0:
		return fmt.Sprintf("snap %q has running hooks (%s), pids: %s",
			err.SnapInfo.InstanceName(), strings.Join(err.busyHookNames, ", "), pids)
	default:
		return fmt.Sprintf("snap %q has running apps or hooks, pids: %s", err.SnapInfo.InstanceName(), pids)
	}
}

func (*BusySnapError) Is(err error) bool {
	_, ok := err.(*BusySnapError)
	return ok
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

// hardEnsureNothingRunningDuringRefresh performs the complete hard refresh interaction.
//
// The check is designed to run late in the refresh pipeline, after stopping
// snap services. At this point non-enduring services should be stopped, hooks
// should no longer run, and applications should be barred from running
// externally (e.g. by using a new inhibition mechanism for snap run).
//
// On success this function returns a flag indicating if the refresh will be forced
// due to inhibition timeout and a locked snap lock, allowing the caller to
// atomically, with regards to "snap-confine", finish any action that required
// the apps and hooks not to be running. In addition, the persistent run
// inhibition lock is established, forcing snap-run to pause and postpone
// startup of applications from the given snap.
//
// In practice, we either inhibit app startup and refresh the snap _or_ inhibit
// the refresh change and continue running existing app processes.
func hardEnsureNothingRunningDuringRefresh(backend managerBackend, st *state.State, snapst *SnapState, snapsup *SnapSetup, info *snap.Info) (bool, *osutil.FileLock, error) {
	var inhibitionTimeout bool
	lock, err := backend.RunInhibitSnapForUnlink(info, runinhibit.HintInhibitedForRefresh, func() error {
		// In case of successful refresh inhibition the snap state is modified
		// to indicate when the refresh was first inhibited. If the first
		// refresh inhibition is outside of a grace period then refresh
		// proceeds regardless of the existing processes.
		var err error
		inhibitionTimeout, err = inhibitRefresh(st, snapst, snapsup, info)
		return err
	})
	return inhibitionTimeout, lock, err
}

// softCheckNothingRunningForRefresh checks if non-service apps are off for a snap refresh.
//
// The check is designed to run early in the refresh pipeline. Before downloading
// or stopping services for the update, that is, that no non-service apps or hooks
// are currently running. This checks if a non-service and non-ignored snap is
// running  while holding the snap lock, which ensures that we are not racing with
// snap-confine, which is starting a new process in the context of the given snap.
//
// In the case that the check fails, the state is modified to reflect when the
// refresh was first postponed. Eventually the check does not fail, even if
// non-service apps are running, because this mechanism only allows postponing
// refreshes for a bounded amount of time.
func softCheckNothingRunningForRefresh(st *state.State, snapst *SnapState, snapsup *SnapSetup, info *snap.Info) error {
	// Grab per-snap lock to prevent new processes from starting. This is
	// sufficient to perform the check, even though individual processes may
	// fork or exit, we will have per-security-tag information about what is
	// running.
	return backend.WithSnapLock(info, func() error {
		// Perform the soft refresh viability check, possibly writing to the state
		// on failure.
		_, err := inhibitRefresh(st, snapst, snapsup, info)
		return err
	})
}
