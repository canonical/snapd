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
	"github.com/snapcore/snapd/testutil"
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
	MapMountPointsInDataDirsToExcludes = mapMountPointsInDataDirsToExcludes
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
	return testutil.Mock(&osRemove, f)
}

func MockSnapstateAll(f func(*state.State) (map[string]*snapstate.SnapState, error)) (restore func()) {
	return testutil.Mock(&snapstateAll, f)
}

func MockSnapstateCurrentInfo(f func(*state.State, string) (*snap.Info, error)) (restore func()) {
	return testutil.Mock(&snapstateCurrentInfo, f)
}

func MockSnapstateCheckChangeConflictMany(f func(*state.State, []string, string) error) (restore func()) {
	return testutil.Mock(&snapstateCheckChangeConflictMany, f)
}

func MockBackendIter(f func(context.Context, func(*backend.Reader) error) error) (restore func()) {
	return testutil.Mock(&backendIter, f)
}

func MockBackendOpen(f func(string, uint64) (*backend.Reader, error)) (restore func()) {
	return testutil.Mock(&backendOpen, f)
}

func MockBackendList(f func(ctx context.Context, setID uint64, snapNames []string) ([]client.SnapshotSet, error)) (restore func()) {
	return testutil.Mock(&backendList, f)
}

func MockBackendRestore(f func(*backend.Reader, context.Context, snap.Revision, []string, backend.Logf, *dirs.SnapDirOptions) (*backend.RestoreState, error)) (restore func()) {
	return testutil.Mock(&backendRestore, f)
}

func MockBackendCheck(f func(*backend.Reader, context.Context, []string) error) (restore func()) {
	return testutil.Mock(&backendCheck, f)
}

func MockBackendRevert(f func(*backend.RestoreState)) (restore func()) {
	return testutil.Mock(&backendRevert, f)
}

func MockBackendCleanup(f func(*backend.RestoreState)) (restore func()) {
	return testutil.Mock(&backendCleanup, f)
}

func MockBackendImport(f func(context.Context, uint64, io.Reader, *backend.ImportFlags) ([]string, error)) (restore func()) {
	return testutil.Mock(&backendImport, f)
}

func MockBackendCleanupAbandonedImports(f func() (int, error)) (restore func()) {
	return testutil.Mock(&backendCleanupAbandonedImports, f)
}

func MockBackendEstimateSnapshotSize(f func(*snap.Info, []string, *dirs.SnapDirOptions) (uint64, error)) (restore func()) {
	return testutil.Mock(&backendEstimateSnapshotSize, f)
}

func MockBackendNewSnapshotExport(f func(ctx context.Context, setID uint64) (se *SnapshotExport, err error)) (restore func()) {
	return testutil.Mock(&backendNewSnapshotExport, f)
}

func MockConfigGetSnapConfig(f func(*state.State, string) (*json.RawMessage, error)) (restore func()) {
	return testutil.Mock(&configGetSnapConfig, f)
}

func MockConfigSetSnapConfig(f func(*state.State, string, *json.RawMessage) error) (restore func()) {
	return testutil.Mock(&configSetSnapConfig, f)
}

// For testing only
func SetLastForgetExpiredSnapshotTime(mgr *SnapshotManager, t time.Time) {
	mgr.lastForgetExpiredSnapshotTime = t
}

func MockGetSnapDirOptions(f func(*state.State, string) (*dirs.SnapDirOptions, error)) (restore func()) {
	return testutil.Mock(&getSnapDirOpts, f)
}

func MockBackendMapSnapDataDirToSnapVar(f func(*snap.Info, *dirs.SnapDirOptions, []string) (map[string]string, error)) (restore func()) {
	return testutil.Mock(&backendMapSnapDataDirToSnapVar, f)
}
