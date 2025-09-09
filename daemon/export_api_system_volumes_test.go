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

package daemon

import (
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

func MockFdeMgrGenerateRecoveryKey(f func(fdemgr *fdestate.FDEManager) (rkey keys.RecoveryKey, keyID string, err error)) (restore func()) {
	return testutil.Mock(&fdeMgrGenerateRecoveryKey, f)
}

func MockFdeMgrCheckRecoveryKey(f func(fdemgr *fdestate.FDEManager, rkey keys.RecoveryKey, containerRoles []string) (err error)) (restore func()) {
	return testutil.Mock(&fdeMgrCheckRecoveryKey, f)
}

func MockFdestateReplaceRecoveryKey(f func(st *state.State, recoveryKeyID string, keyslots []fdestate.KeyslotRef) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&fdestateReplaceRecoveryKey, f)
}

func MockFdestateReplaceProtectedKey(f func(st *state.State, volumesAuth *device.VolumesAuthOptions, keyslotRefs []fdestate.KeyslotRef) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&fdestateReplaceProtectedKey, f)
}

func MockFdestateChangeAuth(f func(st *state.State, authMode device.AuthMode, old string, new string, keyslotRefs []fdestate.KeyslotRef) (*state.TaskSet, error)) (restore func()) {
	return testutil.Mock(&fdestateChangeAuth, f)
}

func MockDevicestateGetVolumeStructuresWithKeyslots(f func(st *state.State) ([]devicestate.VolumeStructureWithKeyslots, error)) (restore func()) {
	return testutil.Mock(&devicestateGetVolumeStructuresWithKeyslots, f)
}
