// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
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
	if errors.Is(err, &asserts.NotFoundError{}) {
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
	if errors.Is(err, &asserts.NotFoundError{}) {
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

	// Try to ensure we have an accurate time before doing any
	// refreshy stuff. Note that this call will not block.
	devMgr := deviceMgr(st)
	maxWait := 10 * time.Minute
	if !devMgr.ntpSyncedOrWaitedLongerThan(maxWait) {
		return false, nil
	}

	// Either we have a serial or we try anyway if we attempted
	// for a while to get a serial, this would allow us to at
	// least upgrade core if that can help.
	if ensureOperationalAttempts(st) >= 3 {
		return true, nil
	}

	// Check model exists, for validity. We always have a model, either
	// seeded or a generic one that ships with snapd.
	_, err := findModel(st)
	if errors.Is(err, state.ErrNoState) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	_, err = findSerial(st, nil)
	if errors.Is(err, state.ErrNoState) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func checkGadgetOrKernel(st *state.State, snapInfo, curInfo *snap.Info, _ snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
	kind := ""
	var snapType snap.Type
	var getName func(*asserts.Model) string
	switch snapInfo.Type() {
	case snap.TypeGadget:
		kind = "gadget"
		snapType = snap.TypeGadget
		getName = (*asserts.Model).Gadget
	case snap.TypeKernel:
		if deviceCtx.IsClassicBoot() {
			return fmt.Errorf("cannot install a kernel snap if classic boot")
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

func checkGadgetValid(st *state.State, snapInfo, _ *snap.Info, snapf snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
	if snapInfo.Type() != snap.TypeGadget {
		// not a gadget, nothing to do
		return nil
	}
	if deviceCtx.ForRemodeling() {
		// in this case the gadget is checked by
		// checkGadgetRemodelCompatible
		return nil
	}

	// do basic precondition checks on the gadget against its model constraints
	_, err := gadget.ReadInfoFromSnapFile(snapf, deviceCtx.Model())
	return err
}

var once sync.Once

func delayedCrossMgrInit() {
	once.Do(func() {
		snapstate.AddCheckSnapCallback(checkGadgetOrKernel)
		snapstate.AddCheckSnapCallback(checkGadgetValid)
		snapstate.AddCheckSnapCallback(checkGadgetRemodelCompatible)
	})
	snapstate.CanAutoRefresh = canAutoRefresh
	snapstate.CanManageRefreshes = CanManageRefreshes
	snapstate.IsOnMeteredConnection = netutil.IsOnMeteredConnection
	snapstate.DeviceCtx = DeviceCtx
	snapstate.RemodelingChange = RemodelingChange
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
	if errors.Is(err, &asserts.NotFoundError{}) {
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
//   - Move the CanManageRefreshes code into the ifstate
//   - Look at the connections and find the connection for snapd-control
//     with the managed attribute
//   - Take the snap from this connection and look at the snapstate to see
//     if that snap has a snap declaration (to ensure it comes from the store)
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

// ResetSession clears the device store session if any.
func ResetSession(st *state.State) error {
	device, err := internal.Device(st)
	if err != nil {
		return err
	}
	if device.SessionMacaroon != "" {
		device.SessionMacaroon = ""
		if err := internal.SetDevice(st, device); err != nil {
			return err
		}
	}
	return nil
}

func getAllRequiredSnapsForModel(model *asserts.Model) *naming.SnapSet {
	reqSnaps := model.RequiredWithEssentialSnaps()
	return naming.NewSnapSet(reqSnaps)
}

var errNoBeforeLocalModificationsEdge = fmt.Errorf("before-local-modifications edge not found")

// extractBeforeLocalModificationsEdgesTs extracts the first, last download
// phase and install phase tasks from a TaskSet
func extractBeforeLocalModificationsEdgesTs(ts *state.TaskSet) (firstDl, lastDl, firstInst, lastInst *state.Task, err error) {
	edgeTask := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	if edgeTask == nil {
		return nil, nil, nil, nil, errNoBeforeLocalModificationsEdge
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

func isNotInstalled(err error) bool {
	_, ok := err.(*snap.NotInstalledError)
	return ok
}

func notInstalled(st *state.State, name string) (bool, error) {
	_, err := snapstate.CurrentInfo(st, name)
	if isNotInstalled(err) {
		return true, nil
	}
	return false, err
}

func installedSnapChannelChanged(st *state.State, modelSnapName, declaredChannel string) (changed bool, err error) {
	if declaredChannel == "" {
		return false, nil
	}
	var ss snapstate.SnapState
	if err := snapstate.Get(st, modelSnapName, &ss); err != nil {
		// this is unexpected as we know the snap exists
		return false, err
	}
	if ss.Current.Local() {
		// currently installed snap has a local revision, since it's
		// unasserted we cannot say whether it needs a change or not
		return false, nil
	}
	if ss.TrackingChannel != declaredChannel {
		return true, nil
	}
	return false, nil
}

func modelSnapChannelFromDefaultOrPinnedTrack(new *asserts.Model, s *asserts.ModelSnap) (string, error) {
	if new.Grade() == asserts.ModelGradeUnset {
		if s == nil {
			// it was possible to not specify the base snap in UC16
			return "", nil
		}
		if (s.SnapType == "kernel" || s.SnapType == "gadget") && s.PinnedTrack != "" {
			return s.PinnedTrack, nil
		}
		return "", nil
	}
	return channel.Full(s.DefaultChannel)
}

// pass both the snap name and the model snap, as it is possible that
// the model snap is nil for UC16 models
type modelSnapsForRemodel struct {
	currentSnap      string
	currentModelSnap *asserts.ModelSnap
	new              *asserts.Model
	newSnap          string
	newModelSnap     *asserts.ModelSnap
}

func (ms *modelSnapsForRemodel) canHaveUC18PinnedTrack() bool {
	return ms.newModelSnap != nil &&
		(ms.newModelSnap.SnapType == "kernel" || ms.newModelSnap.SnapType == "gadget")
}

func remodelEssentialSnapTasks(ctx context.Context, st *state.State, ms modelSnapsForRemodel, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
	userID := 0
	newModelSnapChannel, err := modelSnapChannelFromDefaultOrPinnedTrack(ms.new, ms.newModelSnap)
	if err != nil {
		return nil, err
	}

	addExistingSnapTasks := snapstate.LinkNewBaseOrKernel
	if ms.newModelSnap != nil && ms.newModelSnap.SnapType == "gadget" {
		addExistingSnapTasks = snapstate.SwitchToNewGadget
	}

	if ms.currentSnap == ms.newSnap {
		// new model uses the same base, kernel or gadget snap
		changed := false
		if ms.new.Grade() != asserts.ModelGradeUnset {
			// UC20 models can specify default channel for all snaps
			// including base, kernel and gadget
			changed, err = installedSnapChannelChanged(st, ms.newSnap, newModelSnapChannel)
			if err != nil {
				return nil, err
			}
		} else if ms.canHaveUC18PinnedTrack() {
			// UC18 models could only specify track for the kernel
			// and gadget snaps
			changed = ms.currentModelSnap.PinnedTrack != ms.newModelSnap.PinnedTrack
		}
		if changed {
			// new modes specifies the same snap, but with a new channel
			return snapstateUpdateWithDeviceContext(st, ms.newSnap,
				&snapstate.RevisionOptions{Channel: newModelSnapChannel},
				userID, snapstate.Flags{NoReRefresh: true}, deviceCtx, fromChange)
		}
		return nil, nil
	}

	// new model specifies a different snap
	needsInstall, err := notInstalled(st, ms.newModelSnap.SnapName())
	if err != nil {
		return nil, err
	}
	if needsInstall {
		// which needs to be installed
		return snapstateInstallWithDeviceContext(ctx, st, ms.newSnap,
			&snapstate.RevisionOptions{Channel: newModelSnapChannel},
			userID, snapstate.Flags{}, deviceCtx, fromChange)
	}

	if ms.new.Grade() != asserts.ModelGradeUnset {
		// in UC20+ models, the model can specify a channel for each
		// snap, thus making it possible to change already installed
		// kernel or base snaps
		changed, err := installedSnapChannelChanged(st, ms.newModelSnap.SnapName(), newModelSnapChannel)
		if err != nil {
			return nil, err
		}
		if changed {
			ts, err := snapstateUpdateWithDeviceContext(st, ms.newSnap,
				&snapstate.RevisionOptions{Channel: newModelSnapChannel},
				userID, snapstate.Flags{NoReRefresh: true}, deviceCtx, fromChange)
			if err != nil {
				return nil, err
			}
			if ts != nil {
				if edgeTask := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge); edgeTask != nil {
					// no task is marked as being last
					// before local modifications are
					// introduced, indicating that the
					// update is a simple
					// switch-snap-channel
					return ts, nil
				} else {
					if ms.newModelSnap.SnapType == "kernel" || ms.newModelSnap.SnapType == "base" {
						// in other cases make sure that
						// the kernel or base is linked
						// and available, and that
						// kernel updates boot assets if
						// needed
						ts, err = snapstate.AddLinkNewBaseOrKernel(st, ts)
						if err != nil {
							return nil, err
						}
					} else if ms.newModelSnap.SnapType == "gadget" {
						// gadget snaps may need gadget
						// related tasks such as assets
						// update or command line update
						ts, err = snapstate.AddGadgetAssetsTasks(st, ts)
						if err != nil {
							return nil, err
						}
					}
					return ts, nil
				}
			}
		}
	}
	return addExistingSnapTasks(st, ms.newSnap)
}

// collect all prerequisites of a given snap from its task set
func prereqsFromSnapTaskSet(ts *state.TaskSet) ([]string, error) {
	for _, t := range ts.Tasks() {
		// find the first task that carries snap setup
		sup, err := snapstate.TaskSnapSetup(t)
		if err != nil {
			if !errors.Is(err, state.ErrNoState) {
				return nil, err
			}
			// try the next one
			continue
		}
		var prereqs []string
		if sup.Base != "" {
			prereqs = append(prereqs, sup.Base)
		}
		if len(sup.Prereq) > 0 {
			prereqs = append(prereqs, sup.Prereq...)
		}
		return prereqs, nil
	}
	return nil, fmt.Errorf("internal error: cannot identify task-snap-setup in taskset")
}

func remodelTasks(ctx context.Context, st *state.State, current, new *asserts.Model, deviceCtx snapstate.DeviceContext, fromChange string) ([]*state.TaskSet, error) {
	userID := 0
	var tss []*state.TaskSet

	snapsAccountedFor := make(map[string]bool)
	neededSnaps := make(map[string]bool)

	updateNeededSnapsFromTs := func(ts *state.TaskSet) error {
		prereqs, err := prereqsFromSnapTaskSet(ts)
		if err != nil {
			return err
		}
		for _, p := range prereqs {
			neededSnaps[p] = true
		}
		return nil
	}

	// kernel
	kms := modelSnapsForRemodel{
		currentSnap:      current.Kernel(),
		currentModelSnap: current.KernelSnap(),
		new:              new,
		newSnap:          new.Kernel(),
		newModelSnap:     new.KernelSnap(),
	}
	ts, err := remodelEssentialSnapTasks(ctx, st, kms, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}
	if ts != nil {
		tss = append(tss, ts)
	}
	snapsAccountedFor[new.Kernel()] = true
	// base
	bms := modelSnapsForRemodel{
		currentSnap:      current.Base(),
		currentModelSnap: current.BaseSnap(),
		new:              new,
		newSnap:          new.Base(),
		newModelSnap:     new.BaseSnap(),
	}
	ts, err = remodelEssentialSnapTasks(ctx, st, bms, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}
	if ts != nil {
		tss = append(tss, ts)
	}
	snapsAccountedFor[new.Base()] = true
	// gadget
	gms := modelSnapsForRemodel{
		currentSnap:      current.Gadget(),
		currentModelSnap: current.GadgetSnap(),
		new:              new,
		newSnap:          new.Gadget(),
		newModelSnap:     new.GadgetSnap(),
	}
	ts, err = remodelEssentialSnapTasks(ctx, st, gms, deviceCtx, fromChange)
	if err != nil {
		return nil, err
	}
	if ts != nil {
		tss = append(tss, ts)
		if err := updateNeededSnapsFromTs(ts); err != nil {
			return nil, err
		}
	}
	snapsAccountedFor[new.Gadget()] = true

	// go through all the model snaps, see if there are new required snaps
	// or a track for existing ones needs to be updated
	for _, modelSnap := range new.SnapsWithoutEssential() {
		// TODO|XXX: have methods that take refs directly
		// to respect the snap ids
		currentInfo, err := snapstate.CurrentInfo(st, modelSnap.SnapName())
		needsInstall := false
		if err != nil {
			if !isNotInstalled(err) {
				return nil, err
			}
			if modelSnap.Presence == "required" {
				needsInstall = true
			}
		}
		// default channel can be set only in UC20 models
		newModelSnapChannel, err := modelSnapChannelFromDefaultOrPinnedTrack(new, modelSnap)
		if err != nil {
			return nil, err
		}
		var ts *state.TaskSet
		if needsInstall {
			// If the snap is not installed we need to install it now.
			ts, err = snapstateInstallWithDeviceContext(ctx, st, modelSnap.SnapName(),
				&snapstate.RevisionOptions{Channel: newModelSnapChannel},
				userID,
				snapstate.Flags{Required: true}, deviceCtx, fromChange)
			if err != nil {
				return nil, err
			}
			tss = append(tss, ts)
		} else if currentInfo != nil && newModelSnapChannel != "" {
			// the snap is already installed and has its default
			// channel declared in the model, but the local install
			// may be tracking a different channel
			changed, err := installedSnapChannelChanged(st, modelSnap.SnapName(), newModelSnapChannel)
			if err != nil {
				return nil, err
			}
			if changed {
				ts, err = snapstateUpdateWithDeviceContext(st, modelSnap.SnapName(),
					&snapstate.RevisionOptions{Channel: newModelSnapChannel},
					userID, snapstate.Flags{NoReRefresh: true},
					deviceCtx, fromChange)
				if err != nil {
					return nil, err
				}
				tss = append(tss, ts)
			}
		}
		if currentInfo == nil {
			// snap is not installed, we have a task set then
			if err := updateNeededSnapsFromTs(ts); err != nil {
				return nil, err
			}
		} else {
			// snap is installed already, so we have 2 possible
			// scenarios, one the snap will be updated, in which
			// case we have a task set and should make sure that the
			// prerequisites of the new revision are accounted for,
			// or two, the snap revision is not being modified so
			// grab whatever is required for the current revision
			if ts != nil && ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge) != nil {
				// not a simple task snap-switch-channel, so
				// take the prerequisites needed by the new
				// revision we're updating to
				if err := updateNeededSnapsFromTs(ts); err != nil {
					return nil, err
				}
			} else {
				if currentInfo.Base != "" {
					neededSnaps[currentInfo.Base] = true
				}
				// deal with content providers
				for defProvider := range snap.NeededDefaultProviders(currentInfo) {
					neededSnaps[defProvider] = true
				}
			}
		}
		snapsAccountedFor[modelSnap.SnapName()] = true
	}
	// Now we know what snaps are in the model and whether they have any
	// dependencies. Verify that the model is self contained, in the sense
	// that all prerequisites of the snaps in the model, i.e. bases and
	// default content providers are explicitly listed in the model
	var missingSnaps []string
	for needed := range neededSnaps {
		if !snapsAccountedFor[needed] {
			missingSnaps = append(missingSnaps, needed)
		}
	}
	if len(missingSnaps) != 0 {
		sort.Strings(missingSnaps)
		return nil, fmt.Errorf("cannot remodel with incomplete model, the following snaps are required but not listed: %s", strutil.Quoted(missingSnaps))
	}

	// TODO: fix the prerequisite task getting stuck during a remodel by
	// making it a NOP, but ensure that the dependencies of new snaps are
	// getting installed or updated if needed and are properly ordered wrt
	// to the tasks that require them (eg. run-hooks)

	// Keep track of downloads tasks carrying snap-setup which is needed for
	// recovery system tasks
	var snapSetupTasks []string

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
		// "link,start" is part of "Install" phase which introduces
		// system modifications. The last task of the "Download" phase
		// is marked with LastBeforeLocalModificationsEdge.
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
		downloadStart, downloadLast, installFirst, installLast, err := extractBeforeLocalModificationsEdgesTs(ts)
		if err != nil {
			if err == errNoBeforeLocalModificationsEdge {
				// there is no task in the task set marked with
				// as being last before system modification
				// edge, which can happen when there is a simple
				// channel switch if the snap which is part of
				// remodel has the same revision in the current
				// channel and one that will be used after
				// remodel
				continue
			}
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
		// download is always a first task of the 'download' phase
		snapSetupTasks = append(snapSetupTasks, downloadStart.ID())
	}
	// Make sure the first install waits for the recovery system (only in
	// UC20) which waits for the last download. With this our (simplified)
	// wait chain looks like this:
	//
	// download1
	//   ^- verify1
	//        ^- download2
	//             ^- verify2
	//                  ^- download3
	//                       ^- verify3
	//                            ^- recovery (UC20)
	//                                 ^- install1
	//                                      ^- install2
	//                                           ^- install3
	if firstInstallInChain != nil && lastDownloadInChain != nil {
		firstInstallInChain.WaitFor(lastDownloadInChain)
	}

	recoverySetupTaskID := ""
	if new.Grade() != asserts.ModelGradeUnset {
		// create a recovery when remodeling to a UC20 system, actual
		// policy for possible remodels has already been verified by the
		// caller
		labelBase := timeNow().Format("20060102")
		label, err := pickRecoverySystemLabel(labelBase)
		if err != nil {
			return nil, fmt.Errorf("cannot select non-conflicting label for recovery system %q: %v", labelBase, err)
		}
		createRecoveryTasks, err := createRecoverySystemTasks(st, label, snapSetupTasks)
		if err != nil {
			return nil, err
		}
		if lastDownloadInChain != nil {
			// wait for all snaps that need to be downloaded
			createRecoveryTasks.WaitFor(lastDownloadInChain)
		}
		if firstInstallInChain != nil {
			// when any snap installations need to happen, they
			// should also wait for recovery system to be created
			firstInstallInChain.WaitAll(createRecoveryTasks)
		}
		tss = append(tss, createRecoveryTasks)
		recoverySetupTaskID = createRecoveryTasks.Tasks()[0].ID()
	}

	// Set the new model assertion - this *must* be the last thing done
	// by the change.
	setModel := st.NewTask("set-model", i18n.G("Set new model assertion"))
	for _, tsPrev := range tss {
		setModel.WaitAll(tsPrev)
	}
	if recoverySetupTaskID != "" {
		// set model needs to access information about the recovery
		// system
		setModel.Set("recovery-system-setup-task", recoverySetupTaskID)
	}
	tss = append(tss, state.NewTaskSet(setModel))

	return tss, nil
}

// Remodel takes a new model assertion and generates a change that
// takes the device from the old to the new model or an error if the
// transition is not possible.
//
// TODO:
//   - Check estimated disk size delta
//   - Check all relevant snaps exist in new store
//     (need to check that even unchanged snaps are accessible)
//   - Make sure this works with Core 20 as well, in the Core 20 case
//     we must enforce the default-channels from the model as well
func Remodel(st *state.State, new *asserts.Model) (*state.Change, error) {
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot remodel until fully seeded")
	}

	current, err := findModel(st)
	if err != nil {
		return nil, err
	}

	if _, err := findSerial(st, nil); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, fmt.Errorf("cannot remodel without a serial")
		}
		return nil, err
	}

	if current.Series() != new.Series() {
		return nil, fmt.Errorf("cannot remodel to different series yet")
	}

	// don't allow remodel on classic for now
	if current.Classic() {
		return nil, fmt.Errorf("cannot remodel from classic model")
	}
	if current.Classic() != new.Classic() {
		return nil, fmt.Errorf("cannot remodel across classic and non-classic models")
	}

	// TODO:UC20: ensure we never remodel to a lower
	// grade

	// also disallow remodel from non-UC20 (grade unset) to UC20
	if current.Grade() != new.Grade() {
		if current.Grade() == asserts.ModelGradeUnset && new.Grade() != asserts.ModelGradeUnset {
			// a case of pre-UC20 -> UC20 remodel
			return nil, fmt.Errorf("cannot remodel from pre-UC20 to UC20+ models")
		}
		return nil, fmt.Errorf("cannot remodel from grade %v to grade %v", current.Grade(), new.Grade())
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
	// FIXME: this needs work to switch from core->bases
	if current.Base() == "" && new.Base() != "" {
		return nil, fmt.Errorf("cannot remodel from core to bases yet")
	}

	// Do we do this only for the more complicated cases (anything
	// more than adding required-snaps really)?
	if err := snapstate.CheckChangeConflictRunExclusively(st, "remodel"); err != nil {
		return nil, err
	}

	remodCtx, err := remodelCtx(st, current, new)
	if err != nil {
		return nil, err
	}

	var tss []*state.TaskSet
	switch remodelKind {
	case ReregRemodel:
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
		err := sto.EnsureDeviceSession()
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
	if chg := RemodelingChange(st); chg != nil {
		return nil, &snapstate.ChangeConflictError{
			Message:    "cannot start remodel, clashing with concurrent one",
			ChangeKind: chg.Kind(),
			ChangeID:   chg.ID(),
		}
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

// RemodelingChange returns a remodeling change in progress, if there is one
func RemodelingChange(st *state.State) *state.Change {
	for _, chg := range st.Changes() {
		if !chg.IsReady() && chg.Kind() == "remodel" {
			return chg
		}
	}
	return nil
}

type recoverySystemSetup struct {
	// Label of the recovery system, selected when tasks are created
	Label string `json:"label"`
	// Directory inside the seed filesystem where the recovery system files
	// are kept, typically /run/mnt/ubuntu-seed/systems/<label>, set when
	// tasks are created
	Directory string `json:"directory"`
	// SnapSetupTasks is a list of task IDs that carry snap setup
	// information, relevant only during remodel, set when tasks are created
	SnapSetupTasks []string `json:"snap-setup-tasks"`
}

func pickRecoverySystemLabel(labelBase string) (string, error) {
	systemDirectory := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", labelBase)
	exists, _, err := osutil.DirExists(systemDirectory)
	if err != nil {
		return "", err
	}
	if !exists {
		return labelBase, nil
	}
	// pick alternative, which is named like <label>-<number>
	present, err := filepath.Glob(systemDirectory + "-*")
	if err != nil {
		return "", err
	}
	maxExistingNumber := 0
	for _, existingDir := range present {
		suffix := existingDir[len(systemDirectory)+1:]
		num, err := strconv.Atoi(suffix)
		if err != nil {
			// non numerical suffix?
			continue
		}
		if num > maxExistingNumber {
			maxExistingNumber = num
		}
	}
	return fmt.Sprintf("%s-%d", labelBase, maxExistingNumber+1), nil
}

func createRecoverySystemTasks(st *state.State, label string, snapSetupTasks []string) (*state.TaskSet, error) {
	// precondition check, the directory should not exist yet
	systemDirectory := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", label)
	exists, _, err := osutil.DirExists(systemDirectory)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("recovery system %q already exists", label)
	}

	create := st.NewTask("create-recovery-system", fmt.Sprintf("Create recovery system with label %q", label))
	// the label we want
	create.Set("recovery-system-setup", &recoverySystemSetup{
		Label:     label,
		Directory: systemDirectory,
		// IDs of the tasks carrying snap-setup
		SnapSetupTasks: snapSetupTasks,
	})

	finalize := st.NewTask("finalize-recovery-system", fmt.Sprintf("Finalize recovery system with label %q", label))
	finalize.WaitFor(create)
	// finalize needs to know the label too
	finalize.Set("recovery-system-setup-task", create.ID())
	return state.NewTaskSet(create, finalize), nil
}

func CreateRecoverySystem(st *state.State, label string) (*state.Change, error) {
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot create new recovery systems until fully seeded")
	}
	chg := st.NewChange("create-recovery-system", fmt.Sprintf("Create new recovery system with label %q", label))
	ts, err := createRecoverySystemTasks(st, label, nil)
	if err != nil {
		return nil, err
	}
	chg.AddAll(ts)
	return chg, nil
}

// InstallFinish creates a change that will finish the install for the given
// label and volumes. This includes writing missing volume content, seting
// up the bootloader and installing the kernel.
func InstallFinish(st *state.State, label string, onVolumes map[string]*gadget.Volume) (*state.Change, error) {
	if label == "" {
		return nil, fmt.Errorf("cannot finish install with an empty system label")
	}
	if onVolumes == nil {
		return nil, fmt.Errorf("cannot finish install without volumes data")
	}

	chg := st.NewChange("install-step-finish", fmt.Sprintf("Finish setup of run system for %q", label))
	finishTask := st.NewTask("install-finish", fmt.Sprintf("Finish setup of run system for %q", label))
	finishTask.Set("system-label", label)
	finishTask.Set("on-volumes", onVolumes)
	chg.AddTask(finishTask)

	return chg, nil
}

// InstallSetupStorageEncryption creates a change that will setup the
// storage encryption for the install of the given label and
// volumes.
func InstallSetupStorageEncryption(st *state.State, label string, onVolumes map[string]*gadget.Volume) (*state.Change, error) {
	if label == "" {
		return nil, fmt.Errorf("cannot setup storage encryption with an empty system label")
	}
	if onVolumes == nil {
		return nil, fmt.Errorf("cannot setup storage encryption without volumes data")
	}

	chg := st.NewChange("install-step-setup-storage-encryption", fmt.Sprintf("Setup storage encryption for installing system %q", label))
	setupStorageEncryptionTask := st.NewTask("install-setup-storage-encryption", fmt.Sprintf("Setup storage encryption for installing system %q", label))
	setupStorageEncryptionTask.Set("system-label", label)
	setupStorageEncryptionTask.Set("on-volumes", onVolumes)
	chg.AddTask(setupStorageEncryptionTask)

	return chg, nil
}
