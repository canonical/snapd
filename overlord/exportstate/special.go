// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// toolsToExport contains the list of snapd tools to export.
var toolsToExport = []string{
	"etelpmoc.sh",     // used by tab completion logic
	"info",            // used by re-execution logic
	"snap-confine",    // sometimes used in special cases
	"snapctl",         // used by snaps to talk to snapd
	"snap-discard-ns", // used by snap-confine inside the per-snap mount namespace
	"snap-exec",       // used by snap-confine inside the per-snap mount namespace
	"snap-gdb-shim",   // used by snap run
	"snap-update-ns",  // used by snap-confine inside the per-snap mount namespace
}

func manifestForClassicSystem() *Manifest {
	primaryKey, subKey := manifestKeysForHost()
	return &Manifest{
		PrimaryKey: primaryKey,
		SubKey:     subKey,
		Symlinks:   exportSetSymlinks(primaryKey, subKey, "tools", exportedSnapdToolsFromHost()),
	}
}

func manifestForCoreSystem() *Manifest {
	primaryKey, subKey := manifestKeysForHost()
	return &Manifest{
		PrimaryKey: primaryKey,
		SubKey:     subKey,
	}
}

func manifestKeysForHost() (primaryKey string, subKey string) {
	return "snapd", "host"
}

func exportedSnapdToolsFromHost() []*ExportEntry {
	entries := make([]*ExportEntry, 0, len(toolsToExport))
	for _, tool := range toolsToExport {
		entries = append(entries, NewExportedHostFile(filepath.Join(dirs.DistroLibExecDir, tool), tool))
	}
	return entries
}

func manifestForSnapdSnap(info *snap.Info) *Manifest {
	primaryKey, subKey := manifestKeysForSnapd(info)
	return &Manifest{
		PrimaryKey: primaryKey,
		SubKey:     subKey,
		Symlinks:   exportSetSymlinks(primaryKey, subKey, "tools", exportedSnapToolsFromSnapdOrCore(info)),
	}
}

func manifestKeysForSnapd(info *snap.Info) (primaryKey string, subKey string) {
	return "snapd", info.Revision.String()
}

func exportedSnapToolsFromSnapdOrCore(info *snap.Info) []*ExportEntry {
	entries := make([]*ExportEntry, 0, len(toolsToExport))
	for _, tool := range toolsToExport {
		entries = append(entries, NewExportedSnapFile(info, filepath.Join("usr/lib/snapd", tool), tool))
	}
	return entries
}

func manifestForCoreSnap(info *snap.Info) *Manifest {
	primaryKey, subKey := manifestKeysForCore(info)
	return &Manifest{
		PrimaryKey: primaryKey,
		SubKey:     subKey,
		Symlinks:   exportSetSymlinks(primaryKey, subKey, "tools", exportedSnapToolsFromSnapdOrCore(info)),
	}
}

func manifestKeysForCore(info *snap.Info) (primaryKey string, subKey string) {
	return "snapd", fmt.Sprintf("core_%s", info.Revision)
}

func manifestForRegularSnap(info *snap.Info) *Manifest {
	primaryKey, subKey := manifestKeysForRegularSnap(info)
	return &Manifest{
		PrimaryKey: primaryKey,
		SubKey:     subKey,
		// TODO: eventually get this from the snap.yaml
	}
}

func manifestKeysForRegularSnap(info *snap.Info) (primaryKey string, subKey string) {
	if info.SnapName() == "core" || info.SnapName() == "snapd" {
		panic("internal error, cannot use manifestKeysForRegularSnap with core or snapd")
	}
	primaryKey = info.SnapName() // Instance key goes to subKey
	if info.InstanceKey == "" {
		subKey = info.Revision.String()
	} else {
		subKey = fmt.Sprintf("%s_%s", info.Revision.String(), info.InstanceKey)
	}
	return primaryKey, subKey
}

// XXX: this is named too similarly to functions above but plays a fundamentally different role.
func effectiveManifestKeysForSnapdOrCore(st *state.State) (primaryKey string, subKey string, err error) {
	snapdInfo, coreInfo, err := currentSnapdAndCoreInfo(st)
	if err != nil {
		return "", "", err
	}
	var activeSnapdSubKey string
	var activeCoreSubKey string
	if snapdInfo != nil && snapdInfo.Broken == "" {
		primaryKey, activeSnapdSubKey = manifestKeysForSnapd(snapdInfo)
	}
	if coreInfo != nil && coreInfo.Broken == "" {
		primaryKey, activeCoreSubKey = manifestKeysForCore(coreInfo)
	}
	subKey = electSubKeyForSnapdTools(activeSnapdSubKey, activeCoreSubKey)
	if subKey != "" && primaryKey == "" {
		primaryKey = "snapd"
	}
	return primaryKey, subKey, nil
}

// electSubKeyForSnapdTools returns the subkey to use for snapd tools export set.
//
// The snapd tools export set is special as there are providers from snaps other
// than snapd that need consideration. The result is, in order of preference:
//
// 0) "host" if on classic with disabled re-execution.
// 1) snapd subkey, if available
// 2) core subkey, if available
// 3) "host" subkey, if on classic
//
// If no provider is available then empty subkey is returned.
func electSubKeyForSnapdTools(activeSnapdSubKey, activeCoreSubKey string) string {
	if release.OnClassic && os.Getenv("SNAP_REEXEC") == "0" {
		return "host"
	}
	if subKey := activeSnapdSubKey; subKey != "" {
		return subKey
	}
	if subKey := activeCoreSubKey; subKey != "" {
		return subKey
	}
	if release.OnClassic {
		return "host"
	}
	return ""
}

// currentSnapdAndCoreInfo returns infos of current revisions of snapd and core.
//
// If a given snap is not installed or does not have a current revision then
// nil returned in the corresponding place.
func currentSnapdAndCoreInfo(st *state.State) (snapdInfo *snap.Info, coreInfo *snap.Info, err error) {
	snapdInfo, err = snapstateCurrentInfo(st, "snapd")
	if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
		return nil, nil, err
	}
	coreInfo, err = snapstateCurrentInfo(st, "core")
	if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
		return nil, nil, err
	}
	return snapdInfo, coreInfo, nil
}

// snapstateCurrentInfo is snapstate.CurrentInfo mockable for testing.
var snapstateCurrentInfo = snapstate.CurrentInfo
