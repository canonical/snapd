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
	DeviceModel    *asserts.Model
	OldDeviceModel *asserts.Model
	Remodeling     bool
	CtxStore       snapstate.StoreService
	SysMode        string
	Ground         bool
}

func (dc *TrivialDeviceContext) Model() *asserts.Model {
	return dc.DeviceModel
}

func (dc *TrivialDeviceContext) GroundContext() snapstate.DeviceContext {
	if dc.ForRemodeling() && dc.OldDeviceModel != nil {
		return &TrivialDeviceContext{
			DeviceModel: dc.OldDeviceModel,
			SysMode:     dc.SysMode,
			Ground:      true,
		}
	}
	return &TrivialDeviceContext{
		DeviceModel: dc.DeviceModel,
		SysMode:     dc.SysMode,
		Ground:      true,
	}
}

func (dc *TrivialDeviceContext) Classic() bool {
	return dc.DeviceModel.Classic()
}

func (dc *TrivialDeviceContext) Kernel() string {
	return dc.DeviceModel.Kernel()
}

func (dc *TrivialDeviceContext) Base() string {
	return dc.DeviceModel.Base()
}

func (dc *TrivialDeviceContext) HasModeenv() bool {
	return dc.Model().Grade() != asserts.ModelGradeUnset
}

func (dc *TrivialDeviceContext) RunMode() bool {
	return dc.SystemMode() == "run"
}

func (dc *TrivialDeviceContext) Store() snapstate.StoreService {
	if dc.Ground {
		panic("retrieved ground context is not intended to drive store operations")
	}
	return dc.CtxStore
}

func (dc *TrivialDeviceContext) ForRemodeling() bool {
	return dc.Remodeling
}

func (dc *TrivialDeviceContext) SystemMode() string {
	mode := dc.SysMode
	if mode == "" {
		return "run"
	}
	return mode
}

func MockDeviceModel(model *asserts.Model) (restore func()) {
	var deviceCtx snapstate.DeviceContext
	if model != nil {
		deviceCtx = &TrivialDeviceContext{DeviceModel: model}
	}
	return MockDeviceContext(deviceCtx)
}

func MockDeviceModelAndMode(model *asserts.Model, systemMode string) (restore func()) {
	deviceCtx := &TrivialDeviceContext{DeviceModel: model, SysMode: systemMode}
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
	r1 := ReplaceDeviceCtxHook(deviceCtxHook)
	// for convenience reflect from the context whether there is a
	// remodeling
	r2 := ReplaceRemodelingHook(func(s *state.State) *state.Change {
		if deviceCtx != nil && deviceCtx.ForRemodeling() {
			return s.NewChange("sample", "test remodeling change")
		}

		return nil
	})
	return func() {
		r1()
		r2()
	}
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

func ReplaceRemodelingHook(remodelingHook func(st *state.State) *state.Change) (restore func()) {
	oldHook := snapstate.RemodelingChange
	snapstate.RemodelingChange = remodelingHook
	return func() {
		snapstate.RemodelingChange = oldHook
	}
}
