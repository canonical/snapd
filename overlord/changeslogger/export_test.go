// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package changeslogger

import (
	"github.com/snapcore/snapd/overlord/state"
)

// NewTestManager creates a new Manager with explicit field values for testing.
func NewTestManager(st *state.State, logPath string) *Manager {
	return &Manager{
		state:         st,
		seenChanges:   make(map[string]ChangeInfo),
		changeLogPath: logPath,
	}
}
