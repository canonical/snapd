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
	"github.com/snapcore/snapd/i18n"
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

var (
	snapstateInstall = snapstate.Install
	snapstateUpdate  = snapstate.Update
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

	if snapInfo.InstanceName() != snapInfo.SnapName() {
		return fmt.Errorf("cannot install %q, parallel installation of kernel or gadget snaps is not supported", snapInfo.InstanceName())
	}

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
	return proxyStore(st, config.NewTransaction(st))
}

func proxyStore(st *state.State, tr *config.Transaction) (*asserts.Store, error) {
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
//
// TODO:
// - Move the CanManageRefreshes code into the ifstate
// - Look at the connections and find the connection for snapd-control
//   with the managed attribute
// - Take the snap from this connection and look at the snapstate to see
//   if that snap has a snap declaration (to ensure it comes from the store)
func CanManageRefreshes(st *state.State) bool {
	snapStates, err := snapstate.All(st)
	if err != nil {
		return false
	}
	for _, snapst := range snapStates {
		// Always get the current info even if the snap is currently
		// being operated on or if its disabled.
		info, err := snapst.CurrentInfo()
		if err != nil {
			continue
		}
		if info.Broken != "" {
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

// extractDownloadInstallEdgesFromTs is a helper that extract the first, last
// download and install tasks from a TaskSet
func extractDownloadInstallEdgesFromTs(ts *state.TaskSet) (firstDl, lastDl, firstInst, lastInst *state.Task) {
	edgeTask := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	for _, t := range ts.Tasks() {
		if firstDl == nil {
			firstDl = t
		}
		if firstInst == nil && lastDl != nil {
			firstInst = t
		}
		if t == edgeTask {
			lastDl = t
		}
		if firstInst != nil {
			lastInst = t
		}
	}
	return firstDl, lastDl, firstInst, lastInst
}

// Remodel takes a new model assertion and generates a change that
// takes the device from the old to the new model or an error if the
// transition is not possible.
//
// TODO:
// - Check estimated disk size delta
// - Reapply gadget connections as needed
// - Need new session/serial if changing store or model
// - Check all relevant snaps exist in new store
//   (need to check that even unchanged snaps are accessible)
func Remodel(st *state.State, new *asserts.Model) ([]*state.TaskSet, error) {
	current, err := Model(st)
	if err != nil {
		return nil, err
	}
	if current.Series() != new.Series() {
		return nil, fmt.Errorf("cannot remodel to different series yet")
	}
	// FIXME: we need language in the model assertion to declare
	// what transitions are ok across device services (aka serial
	// vaults) before we allow remodel like this.
	//
	// Right now we only allow "remodel" to a different revision of
	// the same model.
	if current.BrandID() != new.BrandID() {
		return nil, fmt.Errorf("cannot remodel to different brands yet")
	}
	if current.Model() != new.Model() {
		return nil, fmt.Errorf("cannot remodel to different models yet")
	}
	if current.Store() != new.Store() {
		return nil, fmt.Errorf("cannot remodel to different stores yet")
	}
	// TODO: should we restrict remodel from one arch to another?
	// There are valid use-cases here though, i.e. amd64 machine that
	// remodels itself to/from i386 (if the HW can do both 32/64 bit)
	if current.Architecture() != new.Architecture() {
		return nil, fmt.Errorf("cannot remodel to different architectures yet")
	}

	// calculate snap differences between the two models
	// FIXME: this needs work to switch the base to boot as well
	if current.Base() != new.Base() {
		return nil, fmt.Errorf("cannot remodel to different bases yet")
	}
	// FIXME: we need to support this soon but right now only a single
	// snap of type "gadget/kernel" is allowed so this needs work
	if current.Kernel() != new.Kernel() {
		return nil, fmt.Errorf("cannot remodel to different kernels yet")
	}
	if current.Gadget() != new.Gadget() {
		return nil, fmt.Errorf("cannot remodel to different gadgets yet")
	}
	userID := 0

	// adjust kernel track
	var tss []*state.TaskSet
	if current.KernelTrack() != new.KernelTrack() {
		ts, err := snapstateUpdate(st, new.Kernel(), new.KernelTrack(), snap.R(0), userID, snapstate.Flags{NoReRefresh: true})
		if err != nil {
			return nil, err
		}
		tss = append(tss, ts)
	}
	// add new required snaps
	for _, snapName := range new.RequiredSnaps() {
		_, err := snapstate.CurrentInfo(st, snapName)
		// if the snap is not installed we need to install it now
		if _, ok := err.(*snap.NotInstalledError); ok {
			ts, err := snapstateInstall(st, snapName, "", snap.R(0), userID, snapstate.Flags{})
			if err != nil {
				return nil, err
			}
			tss = append(tss, ts)
		} else if err != nil {
			return nil, err
		}
	}
	// TODO: Validate that all bases and default-providers are part
	//       of the install tasksets and error if not. If the
	//       prereq task handler check starts adding installs into
	//       our remodel change our carefully constructed wait chain
	//       breaks down.

	// Ensure all download/check tasks are run *before* the install
	// tasks. During a remodel the network may not be available so
	// we need to ensure we have everything local.
	var lastDownloadInChain, firstInstallInChain *state.Task
	var prevDownload, prevInstall *state.Task
	for _, ts := range tss {
		// make sure all things happen sequentially:
		// - all tasks inside ts{Download,Install} already wait for
		//   each other
		//   Out chains look like this:
		//     install1 <- verify1 <- download1
		//     install2 <- verify2 <- download2
		// - add wait of each first ts{Download,Install} task for
		//   the last previous ts{Download,Install} task
		//   Our chains now looks like:
		//     download1 (no waits)
		//     download2 <- verify1
		//     install1 <- verify1 <- download1
		//     install2 <- verify2 <- install1
		firstDownload, lastDownload, firstInstall, lastInstall := extractDownloadInstallEdgesFromTs(ts)
		if prevDownload != nil {
			firstDownload.WaitFor(prevDownload)
		}
		if prevInstall != nil {
			firstInstall.WaitFor(prevInstall)
		}
		prevDownload = lastDownload
		prevInstall = lastInstall
		// update global state
		lastDownloadInChain = lastDownload
		if firstInstallInChain == nil {
			firstInstallInChain = firstInstall
		}
	}
	// Make sure the first install waits for the last download. With this
	// our (simplified) wait chain looks like:
	// download1 <- verify1 <- download2 <- verify2 <- install1 <- install2
	firstInstallInChain.WaitFor(lastDownloadInChain)

	// Set the new model assertion - this *must* be the last thing done
	// by the change.
	setModel := st.NewTask("set-model", i18n.G("Set new model assertion"))
	setModel.Set("new-model", asserts.Encode(new))
	for _, tsPrev := range tss {
		setModel.WaitAll(tsPrev)
	}
	tss = append(tss, state.NewTaskSet(setModel))

	return tss, nil
}
