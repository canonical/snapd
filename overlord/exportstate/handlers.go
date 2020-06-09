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
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/state"
)

func (m *ExportManager) doExportContent(task *state.Task, tomb *tomb.Tomb) error {
	info, err := m.readInfo(task)
	if err != nil {
		return err
	}
	return m.exportContent(task, info)
}

func (m *ExportManager) undoExportContent(task *state.Task, tomb *tomb.Tomb) error {
	info, err := m.readInfo(task)
	if err != nil {
		return err
	}
	return m.unexportContent(task, info)
}

func (m *ExportManager) doUnexportContent(task *state.Task, tomb *tomb.Tomb) error {
	info, err := m.readInfo(task)
	if err != nil {
		return err
	}
	return m.unexportContent(task, info)
}

func (m *ExportManager) undoUnexportContent(task *state.Task, tomb *tomb.Tomb) error {
	info, err := m.readInfo(task)
	if err != nil {
		return err
	}
	return m.exportContent(task, info)
}
