// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2019 Canonical Ltd
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

package snapstatetest

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

type TrivialDeviceContext struct {
	DeviceModel *asserts.Model
	Remodeling  bool
}

func (dc *TrivialDeviceContext) Model() *asserts.Model {
	return dc.DeviceModel
}

func (dc *TrivialDeviceContext) Store() snapstate.StoreService {
	return nil
}

func (dc *TrivialDeviceContext) ForRemodeling() bool {
	return dc.Remodeling
}

func MockDeviceModel(model *asserts.Model) (restore func()) {
	var deviceCtx snapstate.DeviceContext
	if model != nil {
		deviceCtx = &TrivialDeviceContext{DeviceModel: model}
	}
	return MockDeviceContext(deviceCtx)
}

func MockDeviceContext(deviceCtx snapstate.DeviceContext) (restore func()) {
	deviceCtxHook := func(st *state.State, task *state.Task, providedDeviceCtx snapstate.DeviceContext) (snapstate.DeviceContext, error) {
		if providedDeviceCtx != nil {
			return providedDeviceCtx, nil
		}
		if deviceCtx == nil {
			return nil, state.ErrNoState
		}
		return deviceCtx, nil
	}
	return ReplaceDeviceCtxHook(deviceCtxHook)

}

func ReplaceDeviceCtxHook(deviceCtxHook func(st *state.State, task *state.Task, providedDeviceCtx snapstate.DeviceContext) (snapstate.DeviceContext, error)) (restore func()) {
	oldHook := snapstate.DeviceCtx
	snapstate.DeviceCtx = deviceCtxHook
	return func() {
		snapstate.DeviceCtx = oldHook
	}
}

func UseFallbackDeviceModel() (restore func()) {
	return MockDeviceModel(sysdb.GenericClassicModel())
}
