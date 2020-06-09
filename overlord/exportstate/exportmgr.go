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
	"github.com/snapcore/snapd/snap"
)

const defaultExportDir = "/var/lib/snapd/export"

var ExportDir = defaultExportDir

func init() {
	dirs.AddRootDirCallback(func(root string) {
		ExportDir = filepath.Join(root, defaultExportDir)
	})
}

type ExportManager struct {
	state  *state.State
	runner *state.TaskRunner
}

// Manager returns a new ExportManager.
func Manager(s *state.State, runner *state.TaskRunner) *ExportManager {
	manager := &ExportManager{state: s, runner: runner}
	runner.AddHandler("export-content", manager.doExportContent, manager.undoExportContent)
	runner.AddHandler("unexport-content", manager.doUnexportContent, manager.undoUnexportContent)
	return manager
}

// Ensure implements StateManager.Ensure.
func (m *ExportManager) Ensure() error {
	return nil
}

func (m *ExportManager) readInfo(task *state.Task) (*snap.Info, error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return nil, err
	}
	return snapstate.Info(st, snapsup.InstanceName(), snapsup.Revision())
}

func (m *ExportManager) exportContent(task *state.Task, info *snap.Info) error {
	for path, export := range info.Export {
		// XXX: This is is not aware of /snap vs /var/lib/snapd/snap
		privateName := filepath.Join(info.MountDir(), export.Path)
		publicName := filepath.Join(ExportDir, path)
		if err := os.MkdirAll(filepath.Dir(publicName), 0755); err != nil {
			return err
		}
		switch export.Method {
		case "symlink":
			if err := os.Symlink(privateName, publicName); err != nil {
				return fmt.Errorf("cannot export %q as %q: %v", privateName, publicName, err)
			}
		default:
			return fmt.Errorf("cannot export %q as %q, unsupported export method %s", privateName, publicName, export.Method)
		}
	}
	return nil
}

func (m *ExportManager) unexportContent(task *state.Task, info *snap.Info) error {
	for path, export := range info.Export {
		publicName := filepath.Join(ExportDir, path)
		switch export.Method {
		case "symlink":
			if err := os.Remove(publicName); err != nil {
				return fmt.Errorf("cannot unexport %q: %v", publicName, err)
			}
		default:
			return fmt.Errorf("cannot unexport %q, unsupported export method %s", publicName, export.Method)
		}
	}
	return nil
}
