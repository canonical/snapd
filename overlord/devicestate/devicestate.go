// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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
	"context"
	"fmt"
	"sync"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	snapstateInstallWithDeviceContext = snapstate.InstallWithDeviceContext
	snapstateUpdateWithDeviceContext  = snapstate.UpdateWithDeviceContext
)

// findModel returns the device model assertion.
func findModel(st *state.State) (*asserts.Model, error) {
	device, err := internal.Device(st)
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

// findSerial returns the device serial assertion. device is optional and used instead of the global state if provided.
func findSerial(st *state.State, device *auth.DeviceState) (*asserts.Serial, error) {
	if device == nil {
		var err error
		device, err = internal.Device(st)
		if err != nil {
			return nil, err
		}
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
	_, err := findModel(st)
	if err == state.ErrNoState {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	_, err = findSerial(st, nil)
	if err == state.ErrNoState {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func checkGadgetOrKernel(st *state.State, _ snap.Container, snapInfo, curInfo *snap.Info, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
	kind := ""
	var snapType snap.Type
	var getName func(*asserts.Model) string
	switch snapInfo.GetType() {
	case snap.TypeGadget:
		kind = "gadget"
		snapType = snap.TypeGadget
		getName = (*asserts.Model).Gadget
	case snap.TypeKernel:
		if release.OnClassic {
			return fmt.Errorf("cannot install a kernel snap on classic")
		}

		kind = "kernel"
		snapType = snap.TypeKernel
		getName = (*asserts.Model).Kernel
	default:
		// not a relevant check
		return nil
	}

	model := deviceCtx.Model()

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

	found, err := snapstate.HasSnapOfType(st, snapType)
	if err != nil {
		return fmt.Errorf("cannot detect original %s snap: %v", kind, err)
	}
	if found {
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
	snapstate.DeviceCtx = DeviceCtx
	snapstate.Remodeling = Remodeling
}

// proxyStore returns the store assertion for the proxy store if one is set.
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

func getAllRequiredSnapsForModel(model *asserts.Model) *naming.SnapSet {
	reqSnaps := model.RequiredWithEssentialSnaps()
	return naming.NewSnapSet(reqSnaps)
}

// extractDownloadInstallEdgesFromTs extracts the first, last download
// phase and install phase tasks from a TaskSet
func extractDownloadInstallEdgesFromTs(ts *state.TaskSet) (firstDl, lastDl, firstInst, lastInst *state.Task, err error) {
	edgeTask, err := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	tasks := ts.Tasks()
	// we know we always start with downloads
	firstDl = tasks[0]
	// and always end with installs
	lastInst = tasks[len(tasks)-1]

	var edgeTaskIndex int
	for i, task := range tasks {
		if task == edgeTask {
			edgeTaskIndex = i
			break
		}
	}
	return firstDl, tasks[edgeTaskIndex], tasks[edgeTaskIndex+1], lastInst, nil
}

func remodelTasks(ctx context.Context, st *state.State, current, new *asserts.Model, deviceCtx snapstate.DeviceContext, fromChange string) ([]*state.TaskSet, error) {
	userID := 0
	var tss []*state.TaskSet

	// adjust kernel track
	if current.Kernel() == new.Kernel() && current.KernelTrack() != new.KernelTrack() {
		ts, err := snapstateUpdateWithDeviceContext(st, new.Kernel(), &snapstate.RevisionOptions{Channel: new.KernelTrack()}, userID, snapstate.Flags{NoReRefresh: true}, deviceCtx, fromChange)
		if err != nil {
			return nil, err
		}
		tss = append(tss, ts)
	}
	// add new kernel
	if current.Kernel() != new.Kernel() {
		// TODO: we need to support corner cases here like:
		//  0. start with "old-kernel"
		//  1. remodel to "new-kernel"
		//  2. remodel back to "old-kernel"
		// In step (2) we will get a "already-installed" error
		// here right now (workaround: remove "old-kernel")
		ts, err := snapstateInstallWithDeviceContext(ctx, st, new.Kernel(), &snapstate.RevisionOptions{Channel: new.KernelTrack()}, userID, snapstate.Flags{}, deviceCtx, fromChange)
		if err != nil {
			return nil, err
		}
		tss = append(tss, ts)
	}

	// add new required-snaps, no longer required snaps will be cleaned
	// in "set-model"
	for _, snapRef := range new.RequiredNoEssentialSnaps() {
		// TODO|XXX: have methods that take refs directly
		// to respect the snap ids
		_, err := snapstate.CurrentInfo(st, snapRef.SnapName())
		// If the snap is not installed we need to install it now.
		if _, ok := err.(*snap.NotInstalledError); ok {
			ts, err := snapstateInstallWithDeviceContext(ctx, st, snapRef.SnapName(), nil, userID, snapstate.Flags{Required: true}, deviceCtx, fromChange)
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
		// make sure all things happen sequentially
		// Terminology
		// A <- B means B waits for A
		// "download,verify" are part of the "Download" phase
		// "link,start" is part of "Install" phase
		//
		// - all tasks inside ts{Download,Install} already wait for
		//   each other so the chains look something like this:
		//     download1 <- verify1 <- install1
		//     download2 <- verify2 <- install2
		//     download3 <- verify3 <- install3
		// - add wait of each first ts{Download,Install} task for
		//   the last previous ts{Download,Install} task
		//   Our chains now looks like:
		//     download1 <- verify1 <- install1 (as before)
		//     download2 <- verify2 <- install2 (as before)
		//     download3 <- verify3 <- install3 (as before)
		//     verify1 <- download2 (added)
		//     verify2 <- download3 (added)
		//     install1  <- install2 (added)
		//     install2  <- install3 (added)
		downloadStart, downloadLast, installFirst, installLast, err := extractDownloadInstallEdgesFromTs(ts)
		if err != nil {
			return nil, fmt.Errorf("cannot remodel: %v", err)
		}
		if prevDownload != nil {
			// XXX: we don't strictly need to serialize the download
			downloadStart.WaitFor(prevDownload)
		}
		if prevInstall != nil {
			installFirst.WaitFor(prevInstall)
		}
		prevDownload = downloadLast
		prevInstall = installLast
		// update global state
		lastDownloadInChain = downloadLast
		if firstInstallInChain == nil {
			firstInstallInChain = installFirst
		}
	}
	// Make sure the first install waits for the last download. With this
	// our (simplified) wait chain looks like:
	// download1 <- verify1 <- download2 <- verify2 <- download3 <- verify3 <- install1 <- install2 <- install3
	if firstInstallInChain != nil && lastDownloadInChain != nil {
		firstInstallInChain.WaitFor(lastDownloadInChain)
	}

	// Set the new model assertion - this *must* be the last thing done
	// by the change.
	setModel := st.NewTask("set-model", i18n.G("Set new model assertion"))
	for _, tsPrev := range tss {
		setModel.WaitAll(tsPrev)
	}
	tss = append(tss, state.NewTaskSet(setModel))

	return tss, nil
}

// Remodel takes a new model assertion and generates a change that
// takes the device from the old to the new model or an error if the
// transition is not possible.
//
// TODO:
// - Check estimated disk size delta
// - Reapply gadget connections as needed
// - Check all relevant snaps exist in new store
//   (need to check that even unchanged snaps are accessible)
func Remodel(st *state.State, new *asserts.Model) (*state.Change, error) {
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot remodel until fully seeded")
	}

	current, err := findModel(st)
	if err != nil {
		return nil, err
	}
	if current.Series() != new.Series() {
		return nil, fmt.Errorf("cannot remodel to different series yet")
	}

	// TODO: we need dedicated assertion language to permit for
	// model transitions before we allow cross vault
	// transitions.

	remodelKind := ClassifyRemodel(current, new)

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
	if current.Gadget() != new.Gadget() {
		return nil, fmt.Errorf("cannot remodel to different gadgets yet")
	}

	// TODO: should we run a remodel only while no other change is
	// running?  do we add a task upfront that waits for that to be
	// true? Do we do this only for the more complicated cases
	// (anything more than adding required-snaps really)?

	remodCtx, err := remodelCtx(st, current, new)
	if err != nil {
		return nil, err
	}

	var tss []*state.TaskSet
	switch remodelKind {
	case ReregRemodel:
		// nothing else can be in-flight
		for _, chg := range st.Changes() {
			if !chg.IsReady() {
				return nil, &snapstate.ChangeConflictError{Message: "cannot start complete remodel, other changes are in progress"}
			}
		}

		requestSerial := st.NewTask("request-serial", i18n.G("Request new device serial"))

		prepare := st.NewTask("prepare-remodeling", i18n.G("Prepare remodeling"))
		prepare.WaitFor(requestSerial)
		ts := state.NewTaskSet(requestSerial, prepare)
		tss = []*state.TaskSet{ts}
	case StoreSwitchRemodel:
		sto := remodCtx.Store()
		if sto == nil {
			return nil, fmt.Errorf("internal error: a store switch remodeling should have built a store")
		}
		// ensure a new session accounting for the new brand store
		st.Unlock()
		_, err := sto.EnsureDeviceSession()
		st.Lock()
		if err != nil {
			return nil, fmt.Errorf("cannot get a store session based on the new model assertion: %v", err)
		}
		fallthrough
	case UpdateRemodel:
		var err error
		tss, err = remodelTasks(context.TODO(), st, current, new, remodCtx, "")
		if err != nil {
			return nil, err
		}
	}

	// we potentially released the lock a couple of times here:
	// make sure the current model is essentially the same as when
	// we started
	current1, err := findModel(st)
	if err != nil {
		return nil, err
	}
	if current.BrandID() != current1.BrandID() || current.Model() != current1.Model() || current.Revision() != current1.Revision() {
		return nil, &snapstate.ChangeConflictError{Message: fmt.Sprintf("cannot start remodel, clashing with concurrent remodel to %v/%v (%v)", current1.BrandID(), current1.Model(), current1.Revision())}
	}
	// make sure another unfinished remodel wasn't already setup either
	if Remodeling(st) {
		return nil, &snapstate.ChangeConflictError{Message: "cannot start remodel, clashing with concurrent one"}
	}

	var msg string
	if current.BrandID() == new.BrandID() && current.Model() == new.Model() {
		msg = fmt.Sprintf(i18n.G("Refresh model assertion from revision %v to %v"), current.Revision(), new.Revision())
	} else {
		msg = fmt.Sprintf(i18n.G("Remodel device to %v/%v (%v)"), new.BrandID(), new.Model(), new.Revision())
	}

	chg := st.NewChange("remodel", msg)
	remodCtx.Init(chg)
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	return chg, nil
}

// Remodeling returns true whether there's a remodeling in progress
func Remodeling(st *state.State) bool {
	for _, chg := range st.Changes() {
		if !chg.IsReady() && chg.Kind() == "remodel" {
			return true
		}
	}
	return false
}
