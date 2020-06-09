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

var exportDir = defaultExportDir

func init() {
	dirs.AddRootDirCallback(func(root string) {
		exportDir = filepath.Join(root, defaultExportDir)
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
	// TODO: store each exported file into the state.
	for _, export := range info.NamespaceExports {
		if err := m.exportOne(dirs.SnapMountDirInsideNs, export); err != nil {
			return fmt.Errorf("cannot export %q from snap %s, to mount namespace, as %q: %v",
				export.PrivatePath, info.SnapName(), export.PublicPath, err)
		}
	}
	for _, export := range info.HostExports {
		if err := m.exportOne(dirs.SnapMountDir, export); err != nil {
			return fmt.Errorf("cannot export %q from snap %s, to the host, as %q: %v",
				export.PrivatePath, info.SnapName(), export.PublicPath, err)
		}
	}
	return nil
}
func (m *ExportManager) unexportContent(task *state.Task, info *snap.Info) error {
	// TODO: use the state to know what to unexport from the given snap name.
	for _, export := range info.NamespaceExports {
		if err := m.unexportOne(export); err != nil {
			return fmt.Errorf("cannot unexport %q from snap %s: %v",
				export.PrivatePath, info.SnapName(), err)
		}
	}
	for _, export := range info.HostExports {
		if err := m.unexportOne(export); err != nil {
			return fmt.Errorf("cannot unexport %q from snap %s: %v",
				export.PrivatePath, info.SnapName(), err)
		}
	}
	return nil
}

func (m *ExportManager) exportOne(baseDir string, export *snap.Export) error {
	info := export.Snap
	privateName := filepath.Join(baseDir, info.InstanceName(), info.Revision.String(), export.PrivatePath)
	publicName := filepath.Join(exportDir, export.PublicPath)
	if err := os.MkdirAll(filepath.Dir(publicName), 0755); err != nil {
		return err
	}
	switch export.Method {
	case snap.ExportMethodSymlink:
		// Do we have an existing file?
		fi, err := os.Stat(publicName)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// Verify existing symlink.
		if fi != nil && fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(publicName)
			if err != nil {
				return err
			}
			if target == privateName {
				// Symlink is up-to-date.
				return nil
			}
		}
		// Remove existing file.
		// XXX: This should never happen if we modeled the exported state.
		if fi != nil {
			if err := os.Remove(publicName); err != nil {
				return err
			}
		}
		// Export the current version.
		return os.Symlink(privateName, publicName)
	default:
		return fmt.Errorf("unsupported export method %s", export.Method)
	}
}

func (m *ExportManager) unexportOne(export *snap.Export) error {
	publicPath := filepath.Join(exportDir, export.PublicPath)
	switch export.Method {
	case snap.ExportMethodSymlink:
		if err := os.Remove(publicPath); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported export method %s", export.Method)
	}
	return nil
}
