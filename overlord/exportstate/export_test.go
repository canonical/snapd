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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var (
	// exportstate.go
	ExportedVersionSymlinkPath         = exportedVersionSymlinkPath
	SelectExportedVersionForSnapdTools = selectExportedVersionForSnapdTools
	CurrentSnapdAndCoreInfo            = currentSnapdAndCoreInfo
	UpdateExportedVersion              = updateExportedVersion

	// special.go
	EffectiveExportedVersionForSnapdOrCore = effectiveExportedVersionForSnapdOrCore

	// manifest.go
	ExportedFileSourcePath = exportedFileSourcePath
	ExportedFilePath       = exportedFilePath
	RemoveExportedFiles    = removeExportedFiles
	CreateExportedFiles    = createExportedFiles
	CreateExportedFile     = createExportedFile
	RemoveExportedFile     = removeExportedFile
)

func MockSnapStateCurrentInfo(fn func(st *state.State, snapName string) (*snap.Info, error)) (restore func()) {
	old := snapstateCurrentInfo
	snapstateCurrentInfo = fn
	return func() {
		snapstateCurrentInfo = old
	}
}
