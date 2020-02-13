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

package devicestate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// DeviceCtx picks a device context from state, optional task or an
// optionally pre-provided one. Returns ErrNoState if a model
// assertion is not yet known.
// In particular if task belongs to a remodeling change this will find
// the appropriate remodel context.
func DeviceCtx(st *state.State, task *state.Task, providedDeviceCtx snapstate.DeviceContext) (snapstate.DeviceContext, error) {
	if providedDeviceCtx != nil {
		return providedDeviceCtx, nil
	}
	// use the remodelContext if the task is part of a remodel change
	remodCtx, err := remodelCtxFromTask(task)
	if err == nil {
		return remodCtx, nil
	}
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	modelAs, err := findModel(st)
	if err != nil {
		return nil, err
	}

	devMgr := deviceMgr(st)
	return &modelDeviceContext{groundDeviceContext{
		model:         modelAs,
		operatingMode: devMgr.OperatingMode(),
	}}, nil
}

type groundDeviceContext struct {
	model         *asserts.Model
	operatingMode string
}

func (dc *groundDeviceContext) Model() *asserts.Model {
	return dc.model
}

func (dc *groundDeviceContext) GroundContext() snapstate.DeviceContext {
	return dc
}

func (dc *groundDeviceContext) Store() snapstate.StoreService {
	panic("retrieved ground context is not intended to drive store operations")
}

func (dc *groundDeviceContext) ForRemodeling() bool {
	return false
}

func (dc *groundDeviceContext) OperatingMode() string {
	return dc.operatingMode
}

func (dc groundDeviceContext) Classic() bool {
	return dc.model.Classic()
}

func (dc groundDeviceContext) Kernel() string {
	return dc.model.Kernel()
}

func (dc groundDeviceContext) Base() string {
	return dc.model.Base()
}

func (dc groundDeviceContext) RunMode() bool {
	return dc.operatingMode == "run"
}

// HasModeenv is true if the grade is set
// TODO:UC20: will classic devices with uc20 models have a modeenv? I think so?
func (dc groundDeviceContext) HasModeenv() bool {
	return dc.model.Grade() != asserts.ModelGradeUnset
}

// sanity
var _ snapstate.DeviceContext = &groundDeviceContext{}

type modelDeviceContext struct {
	groundDeviceContext
}

func (dc *modelDeviceContext) Store() snapstate.StoreService {
	return nil
}

// sanity
var _ snapstate.DeviceContext = &modelDeviceContext{}
