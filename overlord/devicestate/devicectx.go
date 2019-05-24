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

// DeviceCtx picks a device context from state, optional task or an optionally pre-provided one. Returns ErrNoState if a model assertion is not yet known.
func DeviceCtx(st *state.State, task *state.Task, providedDeviceCtx snapstate.DeviceContext) (snapstate.DeviceContext, error) {
	if providedDeviceCtx != nil {
		return providedDeviceCtx, nil
	}
	modelAs, err := findModel(st)
	if err != nil {
		return nil, err
	}
	return modelDeviceContext{model: modelAs}, nil
}

type modelDeviceContext struct {
	model *asserts.Model
}

// sanity
var _ snapstate.DeviceContext = modelDeviceContext{}

func (dc modelDeviceContext) Model() *asserts.Model {
	return dc.model
}

func (dc modelDeviceContext) Store() snapstate.StoreService {
	return nil
}

func (dc modelDeviceContext) ForRemodeling() bool {
	return false
}
