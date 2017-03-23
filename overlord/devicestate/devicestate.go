// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// Model returns the device model assertion.
func Model(st *state.State) (*asserts.Model, error) {
	device, err := auth.Device(st)
	if err != nil {
		return nil, err
	}

	if device.Brand == "" || device.Model == "" {
		return nil, state.ErrNoState
	}

	a, err := assertstate.DB(st).Find(asserts.ModelType, map[string]string{
		"series":   release.Series,
		"brand-id": device.Brand,
		"model":    device.Model,
	})
	if err == asserts.ErrNotFound {
		return nil, state.ErrNoState
	}
	if err != nil {
		return nil, err
	}

	return a.(*asserts.Model), nil
}

// Serial returns the device serial assertion.
func Serial(st *state.State) (*asserts.Serial, error) {
	device, err := auth.Device(st)
	if err != nil {
		return nil, err
	}

	if device.Serial == "" {
		return nil, state.ErrNoState
	}

	a, err := assertstate.DB(st).Find(asserts.SerialType, map[string]string{
		"brand-id": device.Brand,
		"model":    device.Model,
		"serial":   device.Serial,
	})
	if err == asserts.ErrNotFound {
		return nil, state.ErrNoState
	}
	if err != nil {
		return nil, err
	}

	return a.(*asserts.Serial), nil
}

// auto-refresh
func canAutoRefresh(st *state.State) (bool, error) {
	// we need to be seeded first
	var seeded bool
	st.Get("seeded", &seeded)
	if !seeded {
		return false, nil
	}

	_, err := Model(st)
	if err == state.ErrNoState {
		// no model, no need to wait for a serial
		// can happen only on classic
		return true, nil
	}
	if err != nil {
		return false, err
	}

	// either we have a serial or we try anyway if we attempted
	// for a while to get a serial, this would allow us to at
	// least upgrade core if that can help
	if ensureOperationalAttempts(st) >= 3 {
		return true, nil
	}

	_, err = Serial(st)
	if err == state.ErrNoState {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, flags snapstate.Flags) error {
	kind := ""
	var currentInfo func(*state.State) (*snap.Info, error)
	var getName func(*asserts.Model) string
	switch snapInfo.Type {
	case snap.TypeGadget:
		kind = "gadget"
		currentInfo = snapstate.GadgetInfo
		getName = (*asserts.Model).Gadget
	case snap.TypeKernel:
		if release.OnClassic {
			return fmt.Errorf("cannot install a kernel snap on classic")
		}

		kind = "kernel"
		currentInfo = snapstate.KernelInfo
		getName = (*asserts.Model).Kernel
	default:
		// not a relevant check
		return nil
	}

	model, err := Model(st)
	if err == state.ErrNoState {
		return fmt.Errorf("cannot install %s without model assertion", kind)
	}
	if err != nil {
		return err
	}

	if snapInfo.SnapID != "" {
		snapDecl, err := assertstate.SnapDeclaration(st, snapInfo.SnapID)
		if err != nil {
			return fmt.Errorf("internal error: cannot find snap declaration for %q: %v", snapInfo.Name(), err)
		}
		publisher := snapDecl.PublisherID()
		if publisher != "canonical" && publisher != model.BrandID() {
			return fmt.Errorf("cannot install %s %q published by %q for model by %q", kind, snapInfo.Name(), publisher, model.BrandID())
		}
	} else {
		logger.Noticef("installing unasserted %s %q", kind, snapInfo.Name())
	}

	currentSnap, err := currentInfo(st)
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("cannot find original %s snap: %v", kind, err)
	}
	if currentSnap != nil {
		// already installed, snapstate takes care
		return nil
	}
	// first installation of a gadget/kernel

	expectedName := getName(model)
	if expectedName == "" { // can happen only on classic
		return fmt.Errorf("cannot install %s snap on classic if not requested by the model", kind)
	}

	if snapInfo.Name() != expectedName {
		return fmt.Errorf("cannot install %s %q, model assertion requests %q", kind, snapInfo.Name(), expectedName)
	}

	return nil
}

func init() {
	snapstate.AddCheckSnapCallback(checkGadgetOrKernel)
	snapstate.CanAutoRefresh = canAutoRefresh
}
