// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func setDeviceFromModelAssertion(st *state.State, device *auth.DeviceState, model *asserts.Model) error {
	device.Brand = model.BrandID()
	device.Model = model.Model()
	return internal.SetDevice(st, device)
}

func gadgetDataFromInfo(info *snap.Info, constraints *gadget.ModelConstraints) (*gadget.GadgetData, error) {
	gi, err := gadget.ReadInfo(info.MountDir(), coreGadgetConstraints)
	if err != nil {
		return nil, err
	}
	return &gadget.GadgetData{Info: gi, RootDir: info.MountDir()}, nil
}
