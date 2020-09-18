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
	"sync"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

// ExportManager is responsible for maintenance of content exported from snaps
// to other snaps or to the host system and, in some cases, for content exported
// from the host system to snaps.
//
// The export manager stores state describing the content exported by each
// particular revision. Content exported by the host is a special case and is
// not stored in the state.
type ExportManager struct {
	state  *state.State
	runner *state.TaskRunner
}

// Manager returns a new ExportManager.
func Manager(state *state.State, runner *state.TaskRunner) (*ExportManager, error) {
	delayedCrossMgrInit()
	m := &ExportManager{
		state:  state,
		runner: runner,
	}
	runner.AddHandler("export-content", m.doExportContent, m.undoExportContent)
	runner.AddHandler("unexport-content", m.doUnexportContent, m.undoUnexportContent)
	return m, nil
}

// StartUp implements StateStarterUp.Startup.
func (m *ExportManager) StartUp() error {
	st := m.state
	st.Lock()
	defer st.Unlock()

	perfTimings := timings.New(map[string]string{"startup": "exportmgr"})
	defer perfTimings.Save(st)

	return m.exportSnapdTools()
}

func (m *ExportManager) exportSnapdTools() error {
	// If the host system has an export manifest, create those files.
	if err := NewManifestForHost().CreateExportedFiles(); err != nil {
		return err
	}
	// If snapd or core snaps are installed but do not have manifests in the
	// statem then export their content. This can happen when snapd or core are
	// upgraded via re-execution from a version that was not aware of exports to
	// one that is.
	for _, snapName := range []string{"snapd", "core"} {
		info, err := snapstateCurrentInfo(m.state, snapName)
		if _, ok := err.(*snap.NotInstalledError); ok {
			// If a snap is not installed them we have nothing to check.
			continue
		}
		if err != nil {
			return err
		}
		var oldManifest Manifest
		if Get(m.state, info.InstanceName(), info.Revision, &oldManifest) == nil {
			// If there is an export manifest then presumably there is also content on disk.
			continue
		}
		newManifest := NewManifestForSnap(info)
		if err := newManifest.CreateExportedFiles(); err != nil {
			return err
		}
		Set(m.state, info.InstanceName(), info.Revision, newManifest)
	}
	snapName, exportedVersion, err := effectiveSnapNameAndExportedVersionForSnapdOrCore(m.state)
	if err != nil {
		return err
	}
	return updateExportedVersion(snapName, exportedVersion)
}

// Ensure implements StateManager.Ensure.
func (m *ExportManager) Ensure() error {
	return nil
}

// LinkSnapParticipant aids in link-snap and unlink-snap tasks across managers.
type LinkSnapParticipant struct{}

// SnapLinkageChanged implements LinkParticipant.SnapLinkageChanged.
func (p *LinkSnapParticipant) SnapLinkageChanged(st *state.State, instanceName string) error {
	snapName, exportedVersion, err := SnapNameAndExportedVersion(st, instanceName)
	if err != nil {
		return err
	}
	return updateExportedVersion(snapName, exportedVersion)
}

var once sync.Once

// delayedCrossMgrInit installs a link participant synchronizing the content
// version exported by each snap as it undergoes link and unlink operations.
func delayedCrossMgrInit() {
	once.Do(func() {
		snapstate.AddLinkSnapParticipant(&LinkSnapParticipant{})
	})
}
