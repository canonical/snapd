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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type (
	SystemVolumesResponse = systemVolumesResponse
	VolumeInfo            = volumeInfo
	KeyslotInfo           = keyslotInfo
)

func MockFdeMgrGetKeyslots(f func(fdemgr *fdestate.FDEManager, keyslotRefs []fdestate.KeyslotRef) ([]fdestate.Keyslot, []fdestate.KeyslotRef, error)) (restore func()) {
	return testutil.Mock(&fdeMgrGetKeyslots, f)
}

func MockFdeMgrGenerateRecoveryKey(f func(fdemgr *fdestate.FDEManager) (rkey keys.RecoveryKey, keyID string, err error)) (restore func()) {
	return testutil.Mock(&fdeMgrGenerateRecoveryKey, f)
}

func MockFdeMgrCheckRecoveryKey(f func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error)) (restore func()) {
	return testutil.Mock(&fdeMgrCheckRecoveryKey, f)
}

func MockFdestateReplaceRecoveryKey(f func(st *state.State, recoveryKeyID string, keyslots []fdestate.KeyslotRef) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&fdestateReplaceRecoveryKey, f)
}

func MockSnapstateGadgetInfo(f func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error)) (restore func()) {
	return testutil.Mock(&snapstateGadgetInfo, f)
}
