// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
package fdestate

import (
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

func (m *FDEManager) doReplaceRecoveryKey(t *state.Task, tomb *tomb.Tomb) error {
	// TODO:FDEM: implement recovery key replacement, this is currently only a
	// mock task for testing.

	// TODO:FDEM:
	//   - this might be a re-run, make task idempotent to be reselient to
	//     abrupt reboot/shutdown.
	//   - distinguish between errors (undo) and pure-reboots (re-run).
	//   - conflict detection for key slot tasks is important because it
	//     reduces the possible states we could end up in.

	return nil
}
