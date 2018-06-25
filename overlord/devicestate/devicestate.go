// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

// Package devicestate implements the manager and state aspects responsible
// for the device identity and policies.
package devicestate

import (
	"fmt"
	"sync"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
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
	if asserts.IsNotFound(err) {
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
	if asserts.IsNotFound(err) {
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

	// Either we have a serial or we try anyway if we attempted
	// for a while to get a serial, this would allow us to at
	// least upgrade core if that can help.
	if ensureOperationalAttempts(st) >= 3 {
		return true, nil
	}

	// Check model exists, for sanity. We always have a model, either
	// seeded or a generic one that ships with snapd.
	_, err := Model(st)
	if err == state.ErrNoState {
		return false, nil
	}
	if err != nil {
		return false, err
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
			return fmt.Errorf("internal error: cannot find snap declaration for %q: %v", snapInfo.InstanceName(), err)
		}
		publisher := snapDecl.PublisherID()
		if publisher != "canonical" && publisher != model.BrandID() {
			return fmt.Errorf("cannot install %s %q published by %q for model by %q", kind, snapInfo.InstanceName(), publisher, model.BrandID())
		}
	} else {
		logger.Noticef("installing unasserted %s %q", kind, snapInfo.InstanceName())
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

	// TODO parallel-install: use instance name, instance name must match the store name
	if snapInfo.InstanceName() != expectedName {
		return fmt.Errorf("cannot install %s %q, model assertion requests %q", kind, snapInfo.InstanceName(), expectedName)
	}

	return nil
}

var once sync.Once

func delayedCrossMgrInit() {
	once.Do(func() {
		snapstate.AddCheckSnapCallback(checkGadgetOrKernel)
	})
	snapstate.CanAutoRefresh = canAutoRefresh
	snapstate.CanManageRefreshes = CanManageRefreshes
	snapstate.IsOnMeteredConnection = netutil.IsOnMeteredConnection
	snapstate.Model = Model
}

// ProxyStore returns the store assertion for the proxy store if one is set.
func ProxyStore(st *state.State) (*asserts.Store, error) {
	tr := config.NewTransaction(st)
	var proxyStore string
	err := tr.GetMaybe("core", "proxy.store", &proxyStore)
	if err != nil {
		return nil, err
	}
	if proxyStore == "" {
		return nil, state.ErrNoState
	}

	a, err := assertstate.DB(st).Find(asserts.StoreType, map[string]string{
		"store": proxyStore,
	})
	if asserts.IsNotFound(err) {
		return nil, state.ErrNoState
	}
	if err != nil {
		return nil, err
	}

	return a.(*asserts.Store), nil
}

// interfaceConnected returns true if the given snap/interface names
// are connected
func interfaceConnected(st *state.State, snapName, ifName string) bool {
	conns, err := ifacerepo.Get(st).Connected(snapName, ifName)
	return err == nil && len(conns) > 0
}

// CanManageRefreshes returns true if the device can be
// switched to the "core.refresh.schedule=managed" mode.
func CanManageRefreshes(st *state.State) bool {
	snapStates, err := snapstate.All(st)
	if err != nil {
		return false
	}
	for _, snapst := range snapStates {
		if !snapst.Active {
			continue
		}
		info, err := snapst.CurrentInfo()
		if err != nil {
			continue
		}
		// The snap must have a snap declaration (implies that
		// its from the store)
		if _, err := assertstate.SnapDeclaration(st, info.SideInfo.SnapID); err != nil {
			continue
		}

		for _, plugInfo := range info.Plugs {
			if plugInfo.Interface == "snapd-control" && plugInfo.Attrs["refresh-schedule"] == "managed" {
				snapName := info.InstanceName()
				plugName := plugInfo.Name
				if interfaceConnected(st, snapName, plugName) {
					return true
				}
			}
		}
	}

	return false
}
