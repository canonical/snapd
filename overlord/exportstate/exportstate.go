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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// ExportDir is the root of the export directory tree.
//
// The directory contains a structure which exposes certain files, known as
// export sets, from snaps to the classic system or to other snaps. The general
// pattern is /var/lib/snapd/export/<primaryKey>/<subKey>/<exportSet>, where
// <primaryKey> is usually the snap name, <subKey> is usually the revision and
// instance key and <exportSet> is the name of a related set of files, usually
// of a common type.
var ExportDir = defaultExportDir

const defaultExportDir = "/var/lib/snapd/export"

func init() {
	dirs.AddRootDirCallback(func(rootDir string) {
		ExportDir = filepath.Join(rootDir, defaultExportDir)
	})
}

// Manifest describes content content exported to snaps or the host.
type Manifest struct {
	Symlinks []SymlinkExport `json:"symlinks,omitempty"`
}

// NewManifest returns the manifest of a given snap.
func NewManifest(info *snap.Info) *Manifest {
	return NewAbstractManifest(info).Materialize()
}

// IsEmpty returns true if a manifest describes no content.
func (m *Manifest) IsEmpty() bool {
	return len(m.Symlinks) == 0
}

// CreateExportedFiles creates all the files constituting the export manifest.
//
// The directory /var/lib/snapd/export/$primaryKey/$subKey is created
// if necessary. For each export set in the manifest, additional sub-directory
// is created and populated with symbolic links pointing to the exported files.
//
// The function is idempotent.
func (m *Manifest) CreateExportedFiles() error {
	for _, s := range m.Symlinks {
		if err := s.Create(); err != nil {
			return err
		}
	}
	return nil
}

// RemoveExportedFiles removes all the files constituting the export state.
//
// In addition the path /var/lib/snapd/export/$primaryKey/$subKey
// is pruned, removing empty directories if possible.
//
// On failure removal continues and the first error is returned.
func (m *Manifest) RemoveExportedFiles() error {
	var firstErr error
	for _, s := range m.Symlinks {
		if err := s.Remove(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SymlinkExport describes content exported as symbolic link.
type SymlinkExport struct {
	PrimaryKey string `json:"primary-key"`
	SubKey     string `json:"sub-key"`
	ExportSet  string `json:"export-set"`
	Name       string `json:"name"`
	Target     string `json:"target"`
}

// PathName returns the full path of the symbolic link.
func (s *SymlinkExport) PathName() string {
	return filepath.Join(ExportDir, s.PrimaryKey, s.SubKey, s.ExportSet, s.Name)
}

// Create creates a symbolic link and necessary directories.
//
// The function is idempotent.
func (s *SymlinkExport) Create() error {
	pathName := s.PathName()
	if err := os.MkdirAll(filepath.Dir(pathName), 0755); err != nil {
		return err
	}
	err := os.Symlink(s.Target, pathName)
	if err != nil && os.IsExist(err) {
		if actualTarget, _ := os.Readlink(pathName); actualTarget == s.Target {
			err = nil
		}
	}
	return err
}

// Remove removes the symbolic link and prunes any directories.
func (s *SymlinkExport) Remove() error {
	if err := os.Remove(s.PathName()); err != nil && !os.IsNotExist(err) {
		return err
	}
	// XXX: or iterate upwards until we reach ExportDir
	os.Remove(filepath.Join(ExportDir, s.PrimaryKey, s.SubKey, s.ExportSet))
	os.Remove(filepath.Join(ExportDir, s.PrimaryKey, s.SubKey))
	os.Remove(filepath.Join(ExportDir, s.PrimaryKey))
	return nil
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

// currentSymlinkPath returns the path of the current subkey symlink for given primaryKey.
func currentSubKeySymlinkPath(primaryKey string) string {
	return filepath.Join(ExportDir, primaryKey, "current")
}

// setCurrentSubKey replaces the "current" symlink for the given primary key to
// point to the given subKey. Appropriate subKey can be computed by
// subKeyForSnap.
//
// The directory structure must already exist on disk, at the time this function
// is called.
func setCurrentSubKey(primaryKey, subKey string) error {
	if err := osutil.AtomicSymlink(subKey, currentSubKeySymlinkPath(primaryKey)); err != nil {
		return fmt.Errorf("cannot set current subkey of %q to %q: %v", primaryKey, subKey, err)
	}
	return nil
}

// removeCurrentSubKey removes the "current" symlink for the given primary key.
func removeCurrentSubKey(primaryKey string) error {
	if err := os.Remove(currentSubKeySymlinkPath(primaryKey)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove current subkey of %q: %v", primaryKey, err)
	}
	return nil
}

// ManifestKeys returns the (primaryKey, subKey) tuple to use as the current
// provider of all the export sets of a given snap. The returned subKey may be
// empty, indicating that given snap has no current revision.
func ManifestKeys(st *state.State, instanceName string) (primaryKey string, subKey string, err error) {
	switch instanceName {
	case "core", "snapd":
		primaryKey, subKey, err = manifestKeysForSnapdOrCore(st)
		if err != nil {
			return "", "", err
		}
	default:
		info, err := snapstateCurrentInfo(st, instanceName)
		if _, ok := err.(*snap.NotInstalledError); err != nil && !ok {
			return "", "", err
		}
		if info == nil || info.Broken != "" {
			primaryKey, _ = snap.SplitInstanceName(instanceName)
			return primaryKey, "", nil
		}
		primaryKey, subKey = manifestKeysForRegularSnap(info)
	}
	return primaryKey, subKey, nil
}

// manifestKeysForRegularSnap returns the subkey to use as the current provider of
// all the export sets of a given snap. The given snap info must be the active
// revision of the given snap.
//
// This function does not work for core and snapd.
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

// manifestKeysForSnapd returns the sub key for all export sets from the snapd snap.
func manifestKeysForSnapd(info *snap.Info) (primaryKey string, subKey string) {
	return "snapd", info.Revision.String()
}

// manifestKeysForCore returns the sub key for all export sets from the core snap.
//
// The return value is the custom core_$revision string.
func manifestKeysForCore(info *snap.Info) (primaryKey string, subKey string) {
	return "snapd", fmt.Sprintf("core_%s", info.Revision)
}

func manifestKeysForSnapdOrCore(st *state.State) (primaryKey string, subKey string, err error) {
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
// 1) snapd subkey, if available
// 2) core subkey, if available
// 3) "host" subkey, if on classic
//
// If no provider is available then empty subkey is returned.
func electSubKeyForSnapdTools(activeSnapdSubKey, activeCoreSubKey string) string {
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

// snapstateCurrentInfo is snapstate.CurrentInfo mockable for testing.
var snapstateCurrentInfo = snapstate.CurrentInfo

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

// ExportSetName designates a group of related files exported by a snap.
type ExportSetName string

// AbstractManifest describes a content exported under a tuple of keys.
//
// PrimaryKey is usually the snap name, without the instance key. SubKey is
// usually the snap revision combined with the instance key.  ExportSets
// describes set of exported files, grouped into topics by export set name.
//
// Exceptions apply when PrimaryKey is "snapd". The SubKey has additional forms,
// incluging "$revision", "core_$revision" and "host". Neither primary nor sub
// keys should be parsed.
type AbstractManifest struct {
	PrimaryKey string
	SubKey     string
	ExportSets map[ExportSetName][]ExportEntry
}

const snapdTools ExportSetName = "tools"

// NewAbstractManifest returns the abstract export manifest for a given snap.
//
// Currently only the core and snapd snaps export any content to the system. As
// such the export manifest is not embedded into the snap meta-data but instead
// computed here.
//
// Both snapd and core snaps export content under the snap name "snapd", using
// the export set name "tools". The revision is mangled, for "snapd" it is used
// directly. For "core" it is transformed to "core_$revision".
func NewAbstractManifest(info *snap.Info) *AbstractManifest {
	// XXX: should we use WellKnownSnapID here? Probably not as this must work for
	// unsigned snaps as well. Alternatively, should we look at snap type, as we
	// have unique values for both snapd and core.
	switch info.SnapName() {
	case "snapd":
		primaryKey, subKey := manifestKeysForSnapd(info)
		return &AbstractManifest{
			PrimaryKey: primaryKey,
			SubKey:     subKey,
			ExportSets: map[ExportSetName][]ExportEntry{
				snapdTools: exportedSnapToolsFromSnapdOrCore(info),
			},
		}
	case "core":
		primaryKey, subKey := manifestKeysForCore(info)
		return &AbstractManifest{
			PrimaryKey: primaryKey,
			SubKey:     subKey,
			ExportSets: map[ExportSetName][]ExportEntry{
				snapdTools: exportedSnapToolsFromSnapdOrCore(info),
			},
		}
	default:
		primaryKey, subKey := manifestKeysForRegularSnap(info)
		return &AbstractManifest{
			PrimaryKey: primaryKey,
			SubKey:     subKey,
		}
	}
}

// Materialize converts an abstract manifest into a manifest.
//
// The resulting manifest can be used to create or remove the files
// corresponding to the exported content. The resulting manifest can
// also be marshaled and unmarshaled.
func (am *AbstractManifest) Materialize() *Manifest {
	var size int
	for _, entries := range am.ExportSets {
		size += len(entries)
	}
	symlinks := make([]SymlinkExport, 0, size)
	for exportSetName, entries := range am.ExportSets {
		for _, entry := range entries {
			var target string
			if entry.IsExportedPathValidInHostMountNS() {
				target = entry.PathInHostMountNS()
			} else {
				target = entry.PathInSnapMountNS()
			}
			symlinks = append(symlinks, SymlinkExport{
				PrimaryKey: am.PrimaryKey,
				SubKey:     am.SubKey,
				ExportSet:  string(exportSetName),
				Name:       entry.PathInExportSet(),
				Target:     target,
			})
		}
	}
	return &Manifest{Symlinks: symlinks}
}

// ExportEntry describes a single file placed in a specific export set.
//
// The original file is described by two paths. PathInHostMountNS is is valid in
// the host mount namespace while PathInSnapMountNS is valid in the per-snap
// mount namespace. The original file is exported by creating a symbolic link
// under PathInExportSet, which is relative to the export set that the entry
// belongs to, to one of the two other paths.
//
// If IsExportedPathValidInHostMountNS returns true then PathInHostMountNS is
// used, if it returns false PathInSnapMountNS is used instead.
//
// This distinction enables exporting files that are consumed by either other
// snaps or by the classic system.
type ExportEntry interface {
	PathInExportSet() string
	PathInHostMountNS() string
	PathInSnapMountNS() string
	IsExportedPathValidInHostMountNS() bool
}
