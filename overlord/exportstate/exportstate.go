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

// Package exportstate implements the manager and state aspects responsible
// for the exporting portions of installed snaps to the system.
package exportstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// ExportDir is the root of the export directory tree.
//
// The directory contains a structure which exposes certain files, known as
// export sets, from snaps to the classic system or to other snaps. The general
// pattern is /var/lib/snapd/export/<snapName>/<exportedVersion>/<exportSet>, where
// <snapName> is usually the snap name, <exportedVersion> is usually the revision and
// instance key and <exportSet> is the name of a related set of files, usually
// of a common type.
var ExportDir = defaultExportDir

const defaultExportDir = "/var/lib/snapd/export"

func init() {
	dirs.AddRootDirCallback(func(rootDir string) {
		ExportDir = filepath.Join(rootDir, defaultExportDir)
	})
}

// stateMapKey returns key used for indexing the map of exported snap content.
func stateMapKey(instanceName string, rev snap.Revision) string {
	return instanceName + "/" + rev.String()
}

// Set remembers export manifest for a particular snap revision.
func Set(st *state.State, instanceName string, rev snap.Revision, m *Manifest) {
	var exports map[string]*json.RawMessage
	if err := st.Get("exports", &exports); err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal exports state: " + err.Error())
	}
	if exports == nil {
		exports = make(map[string]*json.RawMessage)
	}
	key := stateMapKey(instanceName, rev)
	if m == nil {
		delete(exports, key)
	} else {
		data, err := json.Marshal(m)
		if err != nil {
			panic("internal error: cannot marshal export manifest: " + err.Error())
		}
		raw := json.RawMessage(data)
		exports[key] = &raw
	}
	st.Set("exports", exports)
}

// Get recalls export manifest of a particular snap revision.
func Get(st *state.State, instanceName string, rev snap.Revision, m *Manifest) error {
	*m = Manifest{}

	var exports map[string]*json.RawMessage
	if err := st.Get("exports", &exports); err != nil {
		// This can return state.ErrNoState
		return err
	}
	key := stateMapKey(instanceName, rev)
	raw, ok := exports[key]
	if !ok {
		return state.ErrNoState
	}
	// XXX: do we need the address?
	if err := json.Unmarshal([]byte(*raw), &m); err != nil {
		return fmt.Errorf("cannot unmarshal export manifest: %v", err)
	}
	return nil
}

// currentSymlinkPath returns the path of the current exported version symlink
// for given snapName.
func currentExportedVersionSymlinkPath(snapName string) string {
	return filepath.Join(ExportDir, snapName, "current")
}

// setCurrentExportedVersion replaces the "current" symlink for the given snap
// name to point to the given version. Appropriate version can be computed by
// exportedVersionForSnap.
//
// If the symbolic link cannot be created because the export directory does not
// exist no error is reported. This is because this function is most often
// called from link-snap, where it runs unconditionally, but most snaps do not
// have any content to export and the symlink would be dangling.
func setCurrentExportedVersion(snapName, exportedVersion string) error {
	pathName := currentExportedVersionSymlinkPath(snapName)
	if err := osutil.AtomicSymlink(exportedVersion, pathName); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot set currently exported version of %q to %q: %v", snapName, exportedVersion, err)
	}
	return nil
}

// removeCurrentExportedVersion removes the "current" symlink for the given snap name.
func removeCurrentExportedVersion(snapName string) error {
	if err := os.Remove(currentExportedVersionSymlinkPath(snapName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot unset currently exported version of %q: %v", snapName, err)
	}
	return nil
}

// ManifestKeys returns the (snapName, exportedVersion) tuple to use as the current
// provider of all the export sets of a given snap. The returned exportedVersion may be
// empty, indicating that given snap has no current revision.
func ManifestKeys(st *state.State, instanceName string) (snapName string, exportedVersion string, err error) {
	switch instanceName {
	case "core", "snapd":
		snapName, exportedVersion, err = effectiveManifestKeysForSnapdOrCore(st)
		if err != nil {
			return "", "", err
		}
	default:
		info, err := snapstateCurrentInfo(st, instanceName)
		if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
			return "", "", err
		}
		if info == nil || info.Broken != "" {
			snapName, _ = snap.SplitInstanceName(instanceName)
			return snapName, "", nil
		}
		snapName, exportedVersion = manifestKeysForRegularSnap(info)
	}
	return snapName, exportedVersion, nil
}
