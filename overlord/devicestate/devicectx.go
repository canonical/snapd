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
	return &modelDeviceContext{
		model:         modelAs,
		operatingMode: devMgr.OperatingMode(),
	}, nil
}

type modelDeviceContext struct {
	model         *asserts.Model
	operatingMode string
}

// sanity
var _ snapstate.DeviceContext = &modelDeviceContext{}

func (dc *modelDeviceContext) Model() *asserts.Model {
	return dc.model
}

func (dc *modelDeviceContext) OldModel() *asserts.Model {
	return nil
}

func (dc *modelDeviceContext) Store() snapstate.StoreService {
	return nil
}

func (dc *modelDeviceContext) ForRemodeling() bool {
	return false
}

func (dc *modelDeviceContext) OperatingMode() string {
	return dc.operatingMode
}
