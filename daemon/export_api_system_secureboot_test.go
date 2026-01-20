// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package daemon

import (
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type SecurebootRequest = securebootRequest

func MockFdestateEFISecurebootDBUpdatePrepare(
	f func(st *state.State, db fdestate.EFISecurebootKeyDatabase, payload []byte) error,
) (restore func()) {
	restore = testutil.Backup(&fdestateEFISecurebootDBUpdatePrepare)
	fdestateEFISecurebootDBUpdatePrepare = f
	return restore
}

func MockFdestateEFISecurebootDBUpdateCleanup(
	f func(st *state.State) error,
) (restore func()) {
	restore = testutil.Backup(&fdestateEFISecurebootDBUpdateCleanup)
	fdestateEFISecurebootDBUpdateCleanup = f
	return restore
}

func MockFdestateEFISecurebootDBManagerStartup(
	f func(st *state.State) error,
) (restore func()) {
	restore = testutil.Backup(&fdestateEFISecurebootDBManagerStartup)
	fdestateEFISecurebootDBManagerStartup = f
	return restore
}
