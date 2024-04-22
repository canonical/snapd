// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package snapshotstate

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapshotstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	NewSnapshotSetID           = newSnapshotSetID
	AllActiveSnapNames         = allActiveSnapNames
	SnapSummariesInSnapshotSet = snapSummariesInSnapshotSet
	CheckSnapshotConflict      = checkSnapshotConflict
	Filename                   = filename
	DoSave                     = doSave
	DoRestore                  = doRestore
	UndoRestore                = undoRestore
	CleanupRestore             = cleanupRestore
	DoCheck                    = doCheck
	DoForget                   = doForget
	SaveExpiration             = saveExpiration
	ExpiredSnapshotSets        = expiredSnapshotSets
	RemoveSnapshotState        = removeSnapshotState

	SetSnapshotOpInProgress = setSnapshotOpInProgress

	DefaultAutomaticSnapshotExpiration = defaultAutomaticSnapshotExpiration
)

func (summaries snapshotSnapSummaries) AsMaps() []map[string]string {
	out := make([]map[string]string, len(summaries))
	for i, summary := range summaries {
		out[i] = map[string]string{
			"snap":     summary.snap,
			"snapID":   summary.snapID,
			"filename": summary.filename,
			"epoch":    summary.epoch.String(),
		}
	}
	return out
}

func MockOsRemove(f func(string) error) (restore func()) {
	old := osRemove
	osRemove = f
	return func() {
		osRemove = old
	}
}

func MockSnapstateAll(f func(*state.State) (map[string]*snapstate.SnapState, error)) (restore func()) {
	old := snapstateAll
	snapstateAll = f
	return func() {
		snapstateAll = old
	}
}

func MockSnapstateCurrentInfo(f func(*state.State, string) (*snap.Info, error)) (restore func()) {
	old := snapstateCurrentInfo
	snapstateCurrentInfo = f
	return func() {
		snapstateCurrentInfo = old
	}
}

func MockSnapstateCheckChangeConflictMany(f func(*state.State, []string, string) error) (restore func()) {
	old := snapstateCheckChangeConflictMany
	snapstateCheckChangeConflictMany = f
	return func() {
		snapstateCheckChangeConflictMany = old
	}
}

func MockBackendIter(f func(context.Context, func(*backend.Reader) error) error) (restore func()) {
	old := backendIter
	backendIter = f
	return func() {
		backendIter = old
	}
}

func MockBackendOpen(f func(string, uint64) (*backend.Reader, error)) (restore func()) {
	old := backendOpen
	backendOpen = f
	return func() {
		backendOpen = old
	}
}

func MockBackendList(f func(ctx context.Context, setID uint64, snapNames []string) ([]client.SnapshotSet, error)) (restore func()) {
	old := backendList
	backendList = f
	return func() {
		backendList = old
	}
}

func MockBackendRestore(f func(*backend.Reader, context.Context, snap.Revision, []string, backend.Logf, *dirs.SnapDirOptions) (*backend.RestoreState, error)) (restore func()) {
	old := backendRestore
	backendRestore = f
	return func() {
		backendRestore = old
	}
}

func MockBackendCheck(f func(*backend.Reader, context.Context, []string) error) (restore func()) {
	old := backendCheck
	backendCheck = f
	return func() {
		backendCheck = old
	}
}

func MockBackendRevert(f func(*backend.RestoreState)) (restore func()) {
	old := backendRevert
	backendRevert = f
	return func() {
		backendRevert = old
	}
}

func MockBackendCleanup(f func(*backend.RestoreState)) (restore func()) {
	old := backendCleanup
	backendCleanup = f
	return func() {
		backendCleanup = old
	}
}

func MockBackendImport(f func(context.Context, uint64, io.Reader, *backend.ImportFlags) ([]string, error)) (restore func()) {
	old := backendImport
	backendImport = f
	return func() {
		backendImport = old
	}
}

func MockBackenCleanupAbandonedImports(f func() (int, error)) (restore func()) {
	old := backendCleanupAbandonedImports
	backendCleanupAbandonedImports = f
	return func() {
		backendCleanupAbandonedImports = old
	}
}

func MockBackendEstimateSnapshotSize(f func(*snap.Info, []string, *dirs.SnapDirOptions) (uint64, error)) (restore func()) {
	old := backendEstimateSnapshotSize
	backendEstimateSnapshotSize = f
	return func() {
		backendEstimateSnapshotSize = old
	}
}

func MockBackendNewSnapshotExport(f func(ctx context.Context, setID uint64) (se *SnapshotExport, err error)) (restore func()) {
	old := backendNewSnapshotExport
	backendNewSnapshotExport = f
	return func() {
		backendNewSnapshotExport = old
	}
}

func MockConfigGetSnapConfig(f func(*state.State, string) (*json.RawMessage, error)) (restore func()) {
	old := configGetSnapConfig
	configGetSnapConfig = f
	return func() {
		configGetSnapConfig = old
	}
}

func MockConfigSetSnapConfig(f func(*state.State, string, *json.RawMessage) error) (restore func()) {
	old := configSetSnapConfig
	configSetSnapConfig = f
	return func() {
		configSetSnapConfig = old
	}
}

// For testing only
func SetLastForgetExpiredSnapshotTime(mgr *SnapshotManager, t time.Time) {
	mgr.lastForgetExpiredSnapshotTime = t
}

func MockGetSnapDirOptions(f func(*state.State, string) (*dirs.SnapDirOptions, error)) (restore func()) {
	old := getSnapDirOpts
	getSnapDirOpts = f
	return func() {
		getSnapDirOpts = old
	}
}
