// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"strings"
	"sync"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
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
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
)

var (
	snapstateInstallWithDeviceContext     = snapstate.InstallWithDeviceContext
	snapstateInstallPathWithDeviceContext = snapstate.InstallPathWithDeviceContext
	snapstateUpdateWithDeviceContext      = snapstate.UpdateWithDeviceContext
	snapstateSwitch                       = snapstate.Switch
	snapstateUpdatePathWithDeviceContext  = snapstate.UpdatePathWithDeviceContext
	snapstateDownload                     = snapstate.Download
)

// findModel returns the device model assertion.
func findModel(st *state.State) (*asserts.Model, error) {
	device := mylog.Check2(internal.Device(st))

	if device.Brand == "" || device.Model == "" {
		return nil, state.ErrNoState
	}

	a := mylog.Check2(assertstate.DB(st).Find(asserts.ModelType, map[string]string{
		"series":   release.Series,
		"brand-id": device.Brand,
		"model":    device.Model,
	}))
	if errors.Is(err, &asserts.NotFoundError{}) {
		return nil, state.ErrNoState
	}

	return a.(*asserts.Model), nil
}

// findSerial returns the device serial assertion. device is optional and used instead of the global state if provided.
func findSerial(st *state.State, device *auth.DeviceState) (*asserts.Serial, error) {
	if device == nil {
		device = mylog.Check2(internal.Device(st))
	}

	if device.Serial == "" {
		return nil, state.ErrNoState
	}

	a := mylog.Check2(assertstate.DB(st).Find(asserts.SerialType, map[string]string{
		"brand-id": device.Brand,
		"model":    device.Model,
		"serial":   device.Serial,
	}))
	if errors.Is(err, &asserts.NotFoundError{}) {
		return nil, state.ErrNoState
	}

	return a.(*asserts.Serial), nil
}

// findKnownRevisionOfModel returns the model assertion revision if any in the
// assertion database for the given model, otherwise it returns -1.
func findKnownRevisionOfModel(st *state.State, mod *asserts.Model) (modRevision int, err error) {
	a := mylog.Check2(assertstate.DB(st).Find(asserts.ModelType, map[string]string{
		"series":   release.Series,
		"brand-id": mod.BrandID(),
		"model":    mod.Model(),
	}))
	if errors.Is(err, &asserts.NotFoundError{}) {
		return -1, nil
	}

	return a.Revision(), nil
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
	_ := mylog.Check2(findModel(st))
	if errors.Is(err, state.ErrNoState) {
		return false, nil
	}

	_ = mylog.Check2(findSerial(st, nil))
	if errors.Is(err, state.ErrNoState) {
		return false, nil
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
		snapDecl := mylog.Check2(assertstate.SnapDeclaration(st, snapInfo.SnapID))

		publisher := snapDecl.PublisherID()
		if publisher != "canonical" && publisher != model.BrandID() {
			return fmt.Errorf("cannot install %s %q published by %q for model by %q", kind, snapInfo.InstanceName(), publisher, model.BrandID())
		}
	} else {
		logger.Noticef("installing unasserted %s %q", kind, snapInfo.InstanceName())
	}

	found := mylog.Check2(snapstate.HasSnapOfType(st, snapType))

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
	_ := mylog.Check2(gadget.ReadInfoFromSnapFile(snapf, deviceCtx.Model()))
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
	mylog.Check(tr.GetMaybe("core", "proxy.store", &proxyStore))

	if proxyStore == "" {
		return nil, state.ErrNoState
	}

	a := mylog.Check2(assertstate.DB(st).Find(asserts.StoreType, map[string]string{
		"store": proxyStore,
	}))
	if errors.Is(err, &asserts.NotFoundError{}) {
		return nil, state.ErrNoState
	}

	return a.(*asserts.Store), nil
}

// interfaceConnected returns true if the given snap/interface names
// are connected
func interfaceConnected(st *state.State, snapName, ifName string) bool {
	conns := mylog.Check2(ifacerepo.Get(st).Connected(snapName, ifName))
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
	snapStates := mylog.Check2(snapstate.All(st))

	for _, snapst := range snapStates {
		// Always get the current info even if the snap is currently
		// being operated on or if its disabled.
		info := mylog.Check2(snapst.CurrentInfo())

		if info.Broken != "" {
			continue
		}
		mylog.Check2(
			// The snap must have a snap declaration (implies that
			// its from the store)
			assertstate.SnapDeclaration(st, info.SideInfo.SnapID))

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
	device := mylog.Check2(internal.Device(st))

	if device.SessionMacaroon != "" {
		device.SessionMacaroon = ""
		mylog.Check(internal.SetDevice(st, device))

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
	_ := mylog.Check2(snapstate.CurrentInfo(st, name))
	if isNotInstalled(err) {
		return true, nil
	}
	return false, err
}

func installedSnapRevisionChanged(st *state.State, modelSnapName string, requiredRevision snap.Revision) (bool, error) {
	if requiredRevision.Unset() {
		return false, nil
	}

	var ss snapstate.SnapState
	mylog.Check(snapstate.Get(st, modelSnapName, &ss))
	// this is unexpected as we know the snap exists

	if ss.Current.Local() {
		return false, errors.New("cannot determine if unasserted snap revision matches required revision")
	}

	return ss.Current != requiredRevision, nil
}

func installedSnapChannelChanged(st *state.State, modelSnapName, declaredChannel string) (changed bool, err error) {
	if declaredChannel == "" {
		return false, nil
	}
	var ss snapstate.SnapState
	mylog.Check(snapstate.Get(st, modelSnapName, &ss))
	// this is unexpected as we know the snap exists

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
	currentSnap            string
	currentModelSnap       *asserts.ModelSnap
	new                    *asserts.Model
	newSnap                string
	newModelSnap           *asserts.ModelSnap
	newRequiredRevision    snap.Revision
	newModelValidationSets []snapasserts.ValidationSetKey
}

func (ms *modelSnapsForRemodel) canHaveUC18PinnedTrack() bool {
	return ms.newModelSnap != nil &&
		(ms.newModelSnap.SnapType == "kernel" || ms.newModelSnap.SnapType == "gadget")
}

type remodelVariant struct {
	offline        bool
	localSnaps     []*snap.SideInfo
	localSnapPaths []string
}

type pathSideInfo struct {
	localSi *snap.SideInfo
	path    string
}

func (ro *remodelVariant) UpdateWithDeviceContext(st *state.State, snapName string, snapID string, opts *snapstate.RevisionOptions,
	userID int, snapStateFlags snapstate.Flags, tracker snapstate.PrereqTracker,
	deviceCtx snapstate.DeviceContext, fromChange string,
) (*state.TaskSet, error) {
	logger.Debugf("snap %s track changed", snapName)
	if opts == nil {
		opts = &snapstate.RevisionOptions{}
	}

	// if an online context, we can go directly to the store
	if !ro.offline {
		return snapstateUpdateWithDeviceContext(st, snapName, opts,
			userID, snapStateFlags, tracker, deviceCtx, fromChange)
	}

	pathSI := ro.maybeSideInfoAndPathFromID(snapID)

	// if we find the side info in the locally provided snaps, then we can
	// directly call snapstate.UpdatePathWithDeviceContext on it
	if pathSI != nil {
		return snapstateUpdatePathWithDeviceContext(st, pathSI.localSi, pathSI.path, snapName, opts,
			userID, snapStateFlags, tracker, deviceCtx, fromChange)
	}

	// if we cannot find the side info in the locally provided snaps, then we
	// will try to use an already installed snap. if the installed snap does not
	// match the requested revision, then we will return an error. if the snap
	// does match the requested revision, then we will switch the channel to the
	// requested channel. see the comment below about how calling this method in
	// the case where the snap needs neither a revision nor channel change would
	// be a bug.

	// TODO: currently, we only consider the snap revision that currently
	// installed. this should also take into account other revisions that we
	// might have on the system (the revisions in SnapState.Sequence)
	info := mylog.Check2(snapstate.CurrentInfo(st, snapName))

	// this case is unexpected, since UpdateWithDeviceContext should
	// only be called if the snap is already installed

	if opts != nil && !opts.Revision.Unset() && info.Revision != opts.Revision {
		var ss snapstate.SnapState
		mylog.Check(snapstate.Get(st, snapName, &ss))

		// if the current revision isn't the revision that is installed, then
		// look at the previous revisions that we have to see if any of those
		// match
		if ss.Sequence.LastIndex(opts.Revision) == -1 {
			return nil, fmt.Errorf("installed snap %q does not match revision required to be used for offline remodel: %s != %s", snapName, opts.Revision, info.Revision)
		}

		// this won't reach out to the store since we know that we already have
		// the snap revision on disk
		return snapstateUpdateWithDeviceContext(st, snapName, opts,
			userID, snapStateFlags, tracker, deviceCtx, fromChange)
	}

	// this would only occur from programmer error, since
	// UpdateWithDeviceContext should only be called if either the snap revision
	// or channel needs to change. once we get here, we know that the revision
	// is the same, so the channel should be different.
	if opts == nil || opts.Channel == info.Channel {
		return nil, fmt.Errorf("internal error: installed snap %q already on channel %q", snapName, info.Channel)
	}

	// since snapstate.Switch doesn't take a prereq tracker, we need to add
	// it explicitly
	tracker.Add(info)

	return snapstateSwitch(st, snapName, opts)
}

func (ro *remodelVariant) InstallWithDeviceContext(ctx context.Context, st *state.State,
	snapName string, snapID string, opts *snapstate.RevisionOptions, userID int,
	snapStateFlags snapstate.Flags, tracker snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext,
	fromChange string,
) (*state.TaskSet, error) {
	logger.Debugf("snap %s needs install", snapName)
	if opts == nil {
		opts = &snapstate.RevisionOptions{}
	}
	if ro.offline {
		pathSI := ro.maybeSideInfoAndPathFromID(snapID)

		// if we can't find the snap as a locally provided snap, then there is
		// nothing to do but return an error. that is because this method should
		// only be called if the snap is not already installed.
		if pathSI == nil {
			return nil, fmt.Errorf("no snap file provided for %q", snapName)
		}

		return snapstateInstallPathWithDeviceContext(st, pathSI.localSi, pathSI.path, snapName, opts,
			userID, snapStateFlags, tracker, deviceCtx, fromChange)
	}
	return snapstateInstallWithDeviceContext(ctx, st, snapName,
		opts, userID, snapStateFlags, tracker, deviceCtx, fromChange)
}

// maybeSideInfoAndPathFromID returns the SideInfo/path for a given snap ID.
// Note that this will work only for asserted snaps, that is the only case we
// support for remodeling at the moment. If the snap cannot be found, then nil
// is returned.
func (ro *remodelVariant) maybeSideInfoAndPathFromID(id string) *pathSideInfo {
	for i, si := range ro.localSnaps {
		if si.SnapID == id {
			return &pathSideInfo{localSi: ro.localSnaps[i], path: ro.localSnapPaths[i]}
		}
	}
	return nil
}

func revisionOptionsForRemodel(channel string, revision snap.Revision, valsets []snapasserts.ValidationSetKey) *snapstate.RevisionOptions {
	opts := &snapstate.RevisionOptions{
		Channel:  channel,
		Revision: revision,
	}

	if !opts.Revision.Unset() {
		opts.ValidationSets = valsets
	}

	return opts
}

func remodelEssentialSnapTasks(ctx context.Context, st *state.State, ms modelSnapsForRemodel, remodelVar remodelVariant, deviceCtx snapstate.DeviceContext, fromChange string, tracker snapstate.PrereqTracker) (*state.TaskSet, error) {
	userID := 0
	newModelSnapChannel := mylog.Check2(modelSnapChannelFromDefaultOrPinnedTrack(ms.new, ms.newModelSnap))

	revOpts := revisionOptionsForRemodel(newModelSnapChannel, ms.newRequiredRevision, ms.newModelValidationSets)

	var newSnapID string
	// a nil model snap will happen for bases on UC16 models.
	if ms.newModelSnap != nil {
		newSnapID = ms.newModelSnap.SnapID
	}

	if ms.currentSnap == ms.newSnap {
		// new model uses the same base, kernel or gadget snap
		channelChanged := false
		if ms.new.Grade() != asserts.ModelGradeUnset {
			// UC20 models can specify default channel for all snaps
			// including base, kernel and gadget
			channelChanged = mylog.Check2(installedSnapChannelChanged(st, ms.newSnap, newModelSnapChannel))
		} else if ms.canHaveUC18PinnedTrack() {
			// UC18 models could only specify track for the kernel
			// and gadget snaps
			channelChanged = ms.currentModelSnap.PinnedTrack != ms.newModelSnap.PinnedTrack
		}

		revisionChanged := mylog.Check2(installedSnapRevisionChanged(st, ms.newSnap, ms.newRequiredRevision))

		if channelChanged || revisionChanged {
			// new model specifies the same snap, but with a new channel or
			// different revision than the existing one
			return remodelVar.UpdateWithDeviceContext(st, ms.newSnap, newSnapID, revOpts, userID,
				snapstate.Flags{NoReRefresh: true}, tracker, deviceCtx, fromChange,
			)
		}

		// if we are here, then the snap is already installed and does not need
		// any changes. thus, add it to the prereq tracker.
		info := mylog.Check2(snapstate.CurrentInfo(st, ms.currentSnap))

		tracker.Add(info)

		return nil, nil
	}

	// new model specifies a different snap
	needsInstall := mylog.Check2(notInstalled(st, ms.newModelSnap.SnapName()))

	if needsInstall {
		// which needs to be installed
		return remodelVar.InstallWithDeviceContext(ctx, st, ms.newSnap, newSnapID, revOpts, userID,
			snapstate.Flags{}, tracker, deviceCtx, fromChange,
		)
	}

	// in UC20+ models, the model can specify a channel for each
	// snap, thus making it possible to change already installed
	// kernel or base snaps
	channelChanged := false
	if ms.new.Grade() != asserts.ModelGradeUnset {
		channelChanged = mylog.Check2(installedSnapChannelChanged(st, ms.newModelSnap.SnapName(), newModelSnapChannel))
	}

	revisionChanged := mylog.Check2(installedSnapRevisionChanged(st, ms.newSnap, ms.newRequiredRevision))

	if !channelChanged && !revisionChanged {
		// if we are here, the new snap is already installed. thus, add it to
		// the prereq tracker.
		info := mylog.Check2(snapstate.CurrentInfo(st, ms.newSnap))

		tracker.Add(info)

		if ms.newModelSnap != nil && ms.newModelSnap.SnapType == "gadget" {
			return snapstate.SwitchToNewGadget(st, ms.newSnap, fromChange)
		}
		return snapstate.LinkNewBaseOrKernel(st, ms.newSnap, fromChange)
	}

	ts := mylog.Check2(remodelVar.UpdateWithDeviceContext(st,
		ms.newSnap, newSnapID, revOpts, userID,
		snapstate.Flags{NoReRefresh: true}, tracker, deviceCtx, fromChange))

	if edgeTask := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge); edgeTask != nil {
		// no task is marked as being last before local modifications are
		// introduced, indicating that the update is a simple
		// switch-snap-channel
		return ts, nil
	}

	switch ms.newModelSnap.SnapType {
	case "kernel", "base":
		// in other cases make sure that the kernel or base is linked and
		// available, and that kernel updates boot assets if needed
		ts = mylog.Check2(snapstate.AddLinkNewBaseOrKernel(st, ts))

	case "gadget":
		// gadget snaps may need gadget related tasks such as assets update or
		// command line update
		ts = mylog.Check2(snapstate.AddGadgetAssetsTasks(st, ts))

	}
	return ts, nil
}

// tasksForEssentialSnap returns tasks for essential snaps (actually,
// except for the snapd snap).
func tasksForEssentialSnap(ctx context.Context, st *state.State,
	snapType string, current, new *asserts.Model,
	revision snap.Revision, vSetKeys []snapasserts.ValidationSetKey, remodelVar remodelVariant,
	tracker snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string,
) (*state.TaskSet, error) {
	var currentSnap, newSnap string
	var currentModelSnap, newModelSnap *asserts.ModelSnap
	switch snapType {
	case "kernel":
		currentSnap = current.Kernel()
		currentModelSnap = current.KernelSnap()
		newSnap = new.Kernel()
		newModelSnap = new.KernelSnap()
	case "base", "core":
		currentSnap = current.Base()
		currentModelSnap = current.BaseSnap()
		newSnap = new.Base()
		newModelSnap = new.BaseSnap()
	case "gadget":
		currentSnap = current.Gadget()
		currentModelSnap = current.GadgetSnap()
		newSnap = new.Gadget()
		newModelSnap = new.GadgetSnap()
	default:
		return nil, fmt.Errorf("internal error: unexpected type %q", snapType)
	}

	ms := modelSnapsForRemodel{
		currentSnap:            currentSnap,
		currentModelSnap:       currentModelSnap,
		new:                    new,
		newSnap:                newSnap,
		newModelSnap:           newModelSnap,
		newRequiredRevision:    revision,
		newModelValidationSets: vSetKeys,
	}
	ts := mylog.Check2(remodelEssentialSnapTasks(ctx, st, ms, remodelVar, deviceCtx, fromChange, tracker))

	return ts, err
}

func remodelSnapdSnapTasks(
	st *state.State, newModel *asserts.Model, rev snap.Revision,
	vSetKeys []snapasserts.ValidationSetKey, remodelVar remodelVariant,
	tracker snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string,
) (*state.TaskSet, error) {
	// First check if snapd snap is installed at all (might be the case
	// for uc16, which happens for some tests).
	var ss snapstate.SnapState
	mylog.Check(snapstate.Get(st, "snapd", &ss))

	// Implicit new channel if snapd is not explicitly in the model
	newSnapdChannel := "latest/stable"
	essentialSnaps := newModel.EssentialSnaps()
	if essentialSnaps[0].SnapType == "snapd" {
		// snapd can be specified explicitly in the model (UC20+)
		newSnapdChannel = essentialSnaps[0].DefaultChannel
	}

	channelChanged := mylog.Check2(installedSnapChannelChanged(st, "snapd", newSnapdChannel))

	revisionChanged := mylog.Check2(installedSnapRevisionChanged(st, "snapd", rev))

	if channelChanged || revisionChanged {
		revOpts := revisionOptionsForRemodel(newSnapdChannel, rev, vSetKeys)

		userID := 0
		return remodelVar.UpdateWithDeviceContext(st, "snapd", naming.WellKnownSnapID("snapd"), revOpts, userID,
			snapstate.Flags{NoReRefresh: true}, tracker, deviceCtx, fromChange)
	}
	return nil, nil
}

func sortNonEssentialRemodelTaskSetsBasesFirst(snaps []*asserts.ModelSnap) []*asserts.ModelSnap {
	sorted := append([]*asserts.ModelSnap(nil), snaps...)

	orderOfType := func(snapType string) int {
		switch snap.Type(snapType) {
		case snap.TypeBase, snap.TypeOS:
			return -1
		}
		return 1
	}

	sort.Slice(sorted, func(i, j int) bool {
		return orderOfType(sorted[i].SnapType) < orderOfType(sorted[j].SnapType)
	})

	return sorted
}

func remodelTasks(ctx context.Context, st *state.State, current, new *asserts.Model,
	deviceCtx snapstate.DeviceContext, fromChange string, localSnaps []*snap.SideInfo, paths []string, opts RemodelOptions,
) ([]*state.TaskSet, error) {
	logger.Debugf("creating remodeling tasks")
	var tss []*state.TaskSet

	tracker := snap.NewSelfContainedSetPrereqTracker()

	// If local snaps are provided, all needed snaps must be locally
	// provided. We check this flag whenever a snap installation/update is
	// found needed for the remodel.
	remodelVar := remodelVariant{
		offline:        opts.Offline,
		localSnaps:     localSnaps,
		localSnapPaths: paths,
	}

	validationSets := mylog.Check2(verifyModelValidationSets(st, new, opts.Offline, deviceCtx))

	// any snap that has a required revision will be in this map, if the snap's
	// version is unconstrained, then we'll get a default-initialized revision
	// from the map
	snapRevisions := mylog.Check2(validationSets.Revisions())

	vSetKeys := validationSets.Keys()

	// First handle snapd as a special case
	ts := mylog.Check2(remodelSnapdSnapTasks(st, new, snapRevisions["snapd"], vSetKeys, remodelVar, tracker, deviceCtx, fromChange))

	if ts != nil {
		tss = append(tss, ts)
	}

	// TODO: this order is not correct, and needs to be changed to match the
	// order that is described in the comment on essentialSnapsRestartOrder in
	// overlord/snapstate/reboot.go
	//
	// In the order: kernel, boot base, gadget
	for _, modelSnap := range new.EssentialSnaps() {
		if modelSnap.SnapType == "snapd" {
			// Already handled
			continue
		}
		ts := mylog.Check2(tasksForEssentialSnap(ctx, st,
			modelSnap.SnapType, current, new,
			snapRevisions[modelSnap.SnapName()], vSetKeys, remodelVar, tracker, deviceCtx, fromChange))

		if ts != nil {
			tss = append(tss, ts)
		}
	}

	// if base is not set, then core will not be returned in the list of snaps
	// returned by new.EssentialSnaps(). since we know that we are remodeling
	// from a core-based system to a core-based system, then the core snap must
	// be installed. thus, we can safely add it to the prereq tracker. note that
	// moving from a UC16 model to a newer model is not supported.
	if new.Base() == "" {
		currentBase := mylog.Check2(snapstate.CurrentInfo(st, "core"))

		tracker.Add(currentBase)
	}

	// sort the snaps so that we collect the task sets for base snaps first, and
	// then the rest. this prevents a later issue where we attempt to install a
	// snap, but the base is not yet installed.
	snapsWithoutEssential := sortNonEssentialRemodelTaskSetsBasesFirst(new.SnapsWithoutEssential())

	const userID = 0

	// go through all the model snaps, see if there are new required snaps
	// or a track for existing ones needs to be updated
	for _, modelSnap := range snapsWithoutEssential {
		logger.Debugf("adding remodel tasks for non-essential snap %s", modelSnap.Name)

		// TODO|XXX: have methods that take refs directly
		// to respect the snap ids
		currentInfo := mylog.Check2(snapstate.CurrentInfo(st, modelSnap.SnapName()))
		needsInstall := false

		// if the snap isn't already installed, and it isn't required,
		// then there is nothing to do. note that if the snap is installed,
		// we might need to change the channel.

		// default channel can be set only in UC20 models
		newModelSnapChannel := mylog.Check2(modelSnapChannelFromDefaultOrPinnedTrack(new, modelSnap))

		revOpts := revisionOptionsForRemodel(newModelSnapChannel, snapRevisions[modelSnap.SnapName()], vSetKeys)

		if needsInstall {
			ts := mylog.Check2(remodelVar.InstallWithDeviceContext(ctx, st, modelSnap.SnapName(), modelSnap.ID(), revOpts,
				userID, snapstate.Flags{Required: true}, tracker, deviceCtx,
				fromChange))

			tss = append(tss, ts)

			continue
		}

		// the snap is already installed and has its default channel declared in
		// the model, but the local install may be tracking a different channel
		channelChanged := mylog.Check2(installedSnapChannelChanged(st, modelSnap.SnapName(), newModelSnapChannel))

		// the validation-sets might require a specific version of the snap
		revisionChanged := mylog.Check2(installedSnapRevisionChanged(
			st, modelSnap.SnapName(), snapRevisions[modelSnap.SnapName()],
		))

		// snap is installed already, so we have 2 possible scenarios:
		// 1. the snap will be updated (new channel or revision), in which case
		// we should make sure that the prerequisites of the new revision are
		// accounted for
		// 2. the snap channel or revision is not being modified so grab
		// whatever is required for the current revision
		if channelChanged || revisionChanged {
			ts := mylog.Check2(remodelVar.UpdateWithDeviceContext(st,
				modelSnap.SnapName(), modelSnap.ID(),
				revOpts, userID, snapstate.Flags{NoReRefresh: true}, tracker,
				deviceCtx, fromChange,
			))

			tss = append(tss, ts)

			// we can know that the snap's revision was changed by checking for
			// the presence of an edge on the task set that separates tasks that
			// do and do not modify the system. if the edge is present, then the
			// revision was changed, and we need to extract the snap's
			// prerequisites from the task set. the absence of this edge,
			// indicates that only the snap's channel was changed and the
			// revision was unchanged. in this case, we treat the snap as if it
			// were unchanged.
			if ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge) != nil {
				continue
			}
		}

		// if we're here, the snap that is installed is unchanged from the snap
		// that the model requires. the snap may have had a channel change, but
		// that channel change did not result in a revision change.
		tracker.Add(currentInfo)
	}
	mylog.Check(checkRequiredGadgetMatchesModelBase(new, tracker))

	warnings, errs := tracker.Check()
	for _, w := range warnings {
		logger.Noticef("remodel prerequisites warning: %v", w)
	}

	if len(errs) > 0 {
		var builder strings.Builder
		builder.WriteString("cannot remodel to model that is not self contained:")

		for _, err := range errs {
			builder.WriteString("\n  - ")
			builder.WriteString(err.Error())
		}

		return nil, errors.New(builder.String())
	}

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
		downloadStart, downloadLast, installFirst, installLast := mylog.Check5(extractBeforeLocalModificationsEdgesTs(ts))

		// there is no task in the task set marked with
		// as being last before system modification
		// edge, which can happen when there is a simple
		// channel switch if the snap which is part of
		// remodel has the same revision in the current
		// channel and one that will be used after
		// remodel

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

	// hybrid core/classic systems might have a system-seed-null; in that case,
	// we cannot create a recovery system
	hasSystemSeed := mylog.Check2(checkForSystemSeed(st, deviceCtx))

	recoverySetupTaskID := ""
	if new.Grade() != asserts.ModelGradeUnset && hasSystemSeed {
		// create a recovery when remodeling to a UC20 system, actual
		// policy for possible remodels has already been verified by the
		// caller
		labelBase := timeNow().Format("20060102")
		label := mylog.Check2(pickRecoverySystemLabel(labelBase))

		// we don't pass in the list of local snaps here because they are
		// already represented by snapSetupTasks
		createRecoveryTasks := mylog.Check2(createRecoverySystemTasks(st, label, snapSetupTasks, CreateRecoverySystemOptions{
			TestSystem: true,
		}))

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
	mylog.Check(

		// Ensure correct restart boundaries are set on the new task-set.
		snapstate.SetEssentialSnapsRestartBoundaries(st, deviceCtx, tss))

	return tss, nil
}

func checkRequiredGadgetMatchesModelBase(model *asserts.Model, tracker *snap.SelfContainedSetPrereqTracker) error {
	modelBase := model.Base()
	if modelBase == "" {
		modelBase = "core"
	}

	for _, sn := range tracker.Snaps() {
		if sn.Type() != snap.TypeGadget {
			continue
		}

		gadgetBase := sn.Base
		if gadgetBase == "" {
			gadgetBase = "core"
		}

		if gadgetBase != modelBase {
			return fmt.Errorf("cannot remodel with gadget snap that has a different base than the model: %q != %q", gadgetBase, modelBase)
		}
	}
	return nil
}

func verifyModelValidationSets(st *state.State, newModel *asserts.Model, offline bool, deviceCtx snapstate.DeviceContext) (*snapasserts.ValidationSets, error) {
	vSets := mylog.Check2(assertstate.ValidationSetsFromModel(st, newModel, assertstate.FetchValidationSetsOptions{
		Offline: offline,
	}, deviceCtx))
	mylog.Check(checkForInvalidSnapsInModel(newModel, vSets))
	mylog.Check(checkForRequiredSnapsNotRequiredInModel(newModel, vSets))

	return vSets, nil
}

func checkForRequiredSnapsNotRequiredInModel(model *asserts.Model, vSets *snapasserts.ValidationSets) error {
	snapsInModel := make(map[string]bool, len(model.RequiredWithEssentialSnaps()))
	for _, sn := range model.RequiredWithEssentialSnaps() {
		snapsInModel[sn.SnapName()] = true
	}

	for _, sn := range vSets.RequiredSnaps() {
		if !snapsInModel[sn] {
			return fmt.Errorf("missing required snap in model: %s", sn)
		}
	}

	return nil
}

func checkForInvalidSnapsInModel(model *asserts.Model, vSets *snapasserts.ValidationSets) error {
	if len(vSets.Keys()) == 0 {
		return nil
	}

	for _, sn := range model.AllSnaps() {
		if !vSets.CanBePresent(sn) {
			return fmt.Errorf("snap presence is marked invalid by validation set: %s", sn.SnapName())
		}
	}
	return nil
}

func checkForSystemSeed(st *state.State, deviceCtx snapstate.DeviceContext) (bool, error) {
	// on non-classic systems, we will always have a seed partition. this check
	// isn't needed, but it makes testing classic systems simpler.
	if !deviceCtx.Classic() {
		return true, nil
	}

	gadgetData := mylog.Check2(CurrentGadgetData(st, deviceCtx))

	return gadgetData.Info.HasRole(gadget.SystemSeed), nil
}

// RemodelOptions are options for Remodel.
type RemodelOptions struct {
	// Offline is true if the remodel should be done without reaching out to the
	// store. Any snaps needed for the remodel, that are not already installed,
	// should be provided via the parameters to Remodel. Snaps that are already
	// installed will be used if they match the revisions that are required by
	// the model.
	Offline bool
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
func Remodel(st *state.State, new *asserts.Model, localSnaps []*snap.SideInfo, paths []string, opts RemodelOptions) (*state.Change, error) {
	var seeded bool
	mylog.Check(st.Get("seeded", &seeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot remodel until fully seeded")
	}

	if !opts.Offline && len(localSnaps) > 0 {
		return nil, errors.New("cannot do an online remodel with provided local snaps")
	}

	current := mylog.Check2(findModel(st))

	prevRev := mylog.Check2(findKnownRevisionOfModel(st, new))

	if new.Revision() < prevRev {
		return nil, fmt.Errorf("cannot remodel to older revision %d of model %s/%s than last revision %d known to the device", new.Revision(), new.BrandID(), new.Model(), prevRev)
	}

	// TODO: we need dedicated assertion language to permit for
	// model transitions before we allow cross vault
	// transitions.

	remodelKind := ClassifyRemodel(current, new)
	mylog.Check2(findSerial(st, nil))

	// it is allowed to remodel without serial for
	// offline remodels that are update only

	if current.Series() != new.Series() {
		return nil, fmt.Errorf("cannot remodel to different series yet")
	}

	devCtx := mylog.Check2(DeviceCtx(st, nil, nil))

	if devCtx.IsClassicBoot() {
		return nil, fmt.Errorf("cannot remodel from classic (non-hybrid) model")
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

	if new.Base() == "" && current.Base() != "" {
		return nil, errors.New("cannot remodel from UC18+ (using snapd snap) system back to UC16 system (using core snap)")
	}

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
	mylog.Check(

		// Do we do this only for the more complicated cases (anything
		// more than adding required-snaps really)?
		snapstate.CheckChangeConflictRunExclusively(st, "remodel"))

	remodCtx := mylog.Check2(remodelCtx(st, current, new))

	var tss []*state.TaskSet
	switch remodelKind {
	case ReregRemodel:
		if opts.Offline {
			// TODO support this in the future if a serial
			// assertion has been provided by a file. To support
			// this case, we will pass the snaps/paths by setting
			// local-{snaps,paths} in the task.
			return nil, fmt.Errorf("cannot remodel offline to different brand ID / model yet")
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
		mylog.Check(sto.EnsureDeviceSession())
		st.Lock()

		fallthrough
	case UpdateRemodel:
		// TODO: make this case follow the same pattern as ReregRemodel, where
		// we call remodelTasks from inside another task, so that the tasks for
		// the remodel are added to an existing and running change. this will
		// allow us to avoid things like calling snapstate.CheckChangeConflictRunExclusively again.

		tss = mylog.Check2(remodelTasks(context.TODO(), st, current, new, remodCtx, "", localSnaps, paths, opts))

	}

	// we potentially released the lock a couple of times here:
	// make sure the current model is essentially the same as when
	// we started
	current1 := mylog.Check2(findModel(st))

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
	mylog.Check(

		// check for exclusive changes again since we released the lock
		snapstate.CheckChangeConflictRunExclusively(st, "remodel"))

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
	// SnapSetupTasks is a list of task IDs that carry snap setup information.
	// Tasks could come from a remodel, or from downloading snaps that were
	// required by a validation set.
	SnapSetupTasks []string `json:"snap-setup-tasks"`
	// LocalSnaps is a list of snaps that should be used to create the recovery
	// system.
	LocalSnaps []LocalSnap `json:"local-snaps,omitempty"`
	// TestSystem is set to true if the new recovery system should
	// not be verified by rebooting into the new system. Once the system is
	// created, it will immediately be considered a valid recovery system.
	TestSystem bool `json:"test-system,omitempty"`
	// MarkDefault is set to true if the new recovery system should be marked as
	// the default recovery system.
	MarkDefault bool `json:"mark-default,omitempty"`
}

func pickRecoverySystemLabel(labelBase string) (string, error) {
	systemDirectory := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", labelBase)
	exists, _ := mylog.Check3(osutil.DirExists(systemDirectory))

	if !exists {
		return labelBase, nil
	}
	// pick alternative, which is named like <label>-<number>
	present := mylog.Check2(filepath.Glob(systemDirectory + "-*"))

	maxExistingNumber := 0
	for _, existingDir := range present {
		suffix := existingDir[len(systemDirectory)+1:]
		num := mylog.Check2(strconv.Atoi(suffix))

		// non numerical suffix?

		if num > maxExistingNumber {
			maxExistingNumber = num
		}
	}
	return fmt.Sprintf("%s-%d", labelBase, maxExistingNumber+1), nil
}

type removeRecoverySystemSetup struct {
	Label string `json:"label"`
}

func removeRecoverySystemTasks(st *state.State, label string) (*state.TaskSet, error) {
	remove := st.NewTask("remove-recovery-system", fmt.Sprintf("Remove recovery system with label %q", label))
	remove.Set("remove-recovery-system-setup", &removeRecoverySystemSetup{
		Label: label,
	})

	return state.NewTaskSet(remove), nil
}

func createRecoverySystemTasks(st *state.State, label string, snapSetupTasks []string, opts CreateRecoverySystemOptions) (*state.TaskSet, error) {
	// precondition check, the directory should not exist yet
	systemDirectory := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", label)
	exists, _ := mylog.Check3(osutil.DirExists(systemDirectory))

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
		LocalSnaps:     opts.LocalSnaps,
		TestSystem:     opts.TestSystem,
		MarkDefault:    opts.MarkDefault,
	})

	ts := state.NewTaskSet(create)

	if opts.TestSystem {
		// Create recovery system requires us to boot into it before finalize
		restart.MarkTaskAsRestartBoundary(create, restart.RestartBoundaryDirectionDo)

		finalize := st.NewTask("finalize-recovery-system", fmt.Sprintf("Finalize recovery system with label %q", label))
		finalize.WaitFor(create)
		// finalize needs to know the label too
		finalize.Set("recovery-system-setup-task", create.ID())

		ts.AddTask(finalize)
	}

	return ts, nil
}

// LocalSnap is a pair of a snap.SideInfo and a path to the snap file on disk
// that is represented by the snap.SideInfo.
type LocalSnap struct {
	// SideInfo is the snap.SideInfo struct that represents a local snap that
	// will be used to create a recovery system.
	SideInfo *snap.SideInfo

	// Path is the path on disk to a snap that will be used to create a recovery
	// system.
	Path string
}

// CreateRecoverySystemOptions is the set of options that can be used with
// CreateRecoverySystem.
type CreateRecoverySystemOptions struct {
	// ValidationSets is a list of validation sets to use when creating the new
	// recovery system. If provided, all snaps used to create recovery system
	// will follow the constraints imposed by the validation sets. If required
	// snaps are not present on the system, and LocalSnapSideInfos is not
	// provided, then the snaps will be downloaded.
	ValidationSets []*asserts.ValidationSet

	// LocalSnaps is an optional list of snaps that will be used to create
	// the new recovery system. If provided, this list must contain any snap
	// that is not already installed that will be needed by the new recovery
	// system.
	LocalSnaps []LocalSnap

	// TestSystem is set to true if the new recovery system should be verified
	// by rebooting into the new system, prior to marking it as a valid recovery
	// system. If false, the system will immediately be considered a valid
	// recovery system.
	TestSystem bool

	// MarkDefault is set to true if the new recovery system should be marked as
	// the default recovery system.
	MarkDefault bool

	// Offline is true if the recovery system should be created without reaching
	// out to the store. Offline must be set to true if LocalSnaps is provided.
	Offline bool
}

var ErrNoRecoverySystem = errors.New("recovery system does not exist")

// RemoveRecoverySystem removes the recovery system with the given label. The
// current recovery system cannot be removed.
func RemoveRecoverySystem(st *state.State, label string) (*state.Change, error) {
	mylog.Check(snapstate.CheckChangeConflictRunExclusively(st, "remove-recovery-system"))

	recoverySystemsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems")
	exists, _ := mylog.Check3(osutil.DirExists(filepath.Join(recoverySystemsDir, label)))

	if !exists {
		return nil, fmt.Errorf("%q not found: %w", label, ErrNoRecoverySystem)
	}

	chg := st.NewChange("remove-recovery-system", fmt.Sprintf("Remove recovery system with label %q", label))

	removeTS := mylog.Check2(removeRecoverySystemTasks(st, label))

	chg.AddAll(removeTS)

	return chg, nil
}

// CreateRecoverySystem creates a new recovery system with the given label. See
// CreateRecoverySystemOptions for details on the options that can be provided.
func CreateRecoverySystem(st *state.State, label string, opts CreateRecoverySystemOptions) (chg *state.Change, err error) {
	mylog.Check(snapstate.CheckChangeConflictRunExclusively(st, "create-recovery-system"))

	if !opts.Offline && len(opts.LocalSnaps) > 0 {
		return nil, errors.New("locally provided snaps cannot be provided when creating a recovery system online")
	}

	var seeded bool
	mylog.Check(st.Get("seeded", &seeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot create new recovery systems until fully seeded")
	}

	model := mylog.Check2(findModel(st))

	valsets := mylog.Check2(assertstate.TrackedEnforcedValidationSetsForModel(st, model))

	for _, vs := range opts.ValidationSets {
		valsets.Add(vs)
	}
	mylog.Check(valsets.Conflict())

	revisions := mylog.Check2(valsets.Revisions())
	mylog.Check(

		// TODO: this restriction should be lifted eventually (in the case that we
		// have a dangerous model), and we should fall back to using snap names in
		// places that IDs are used
		checkForSnapIDs(model, opts.LocalSnaps))
	mylog.Check(

		// check that all snaps from the model are valid in the validation sets
		checkForInvalidSnapsInModel(model, valsets))

	tracker := snap.NewSelfContainedSetPrereqTracker()

	var downloadTSS []*state.TaskSet
	for _, sn := range model.AllSnaps() {
		rev := revisions[sn.Name]

		needsInstall := mylog.Check2(snapNeedsInstall(st, sn.Name, rev))

		if !needsInstall {
			info := mylog.Check2(snapstate.CurrentInfo(st, sn.Name))

			tracker.Add(info)
			continue
		}

		if sn.Presence != "required" {
			sets, _ := mylog.Check3(valsets.CheckPresenceRequired(sn))

			// snap isn't already installed, and it isn't required by model or
			// any validation sets, so we should skip it
			if len(sets) == 0 {
				continue
			}
		}

		if opts.Offline {
			info := mylog.Check2(offlineSnapInfo(sn, rev, opts))

			tracker.Add(info)
			continue
		}

		const userID = 0
		// TODO: this respects the passed in validation sets, but does not
		// currently respect refresh-control style of constraining snap
		// revisions.
		ts, info := mylog.Check3(snapstateDownload(context.TODO(), st, sn.Name, dirs.SnapBlobDir, &snapstate.RevisionOptions{
			Channel:        sn.DefaultChannel,
			Revision:       rev,
			ValidationSets: valsets.Keys(),
		}, userID, snapstate.Flags{}, nil))

		tracker.Add(info)
		downloadTSS = append(downloadTSS, ts)
	}

	warnings, errs := tracker.Check()
	for _, w := range warnings {
		logger.Noticef("create recovery system prerequisites warning: %v", w)
	}

	// TODO: use function from other branch
	if len(errs) > 0 {
		var builder strings.Builder
		builder.WriteString("cannot create recovery system from model that is not self-contained:")

		for _, err := range errs {
			builder.WriteString("\n  - ")
			builder.WriteString(err.Error())
		}

		return nil, errors.New(builder.String())
	}

	var snapsupTaskIDs []string
	if len(downloadTSS) > 0 {
		snapsupTaskIDs = mylog.Check2(extractSnapSetupTaskIDs(downloadTSS))
	}

	chg = st.NewChange("create-recovery-system", fmt.Sprintf("Create new recovery system with label %q", label))
	createTS := mylog.Check2(createRecoverySystemTasks(st, label, snapsupTaskIDs, opts))

	chg.AddAll(createTS)

	for _, ts := range downloadTSS {
		createTS.WaitAll(ts)
		chg.AddAll(ts)
	}

	return chg, nil
}

func checkForSnapIDs(model *asserts.Model, localSnaps []LocalSnap) error {
	for _, sn := range model.AllSnaps() {
		if sn.ID() == "" {
			return fmt.Errorf("cannot create recovery system from model with snap that has no snap id: %q", sn.Name)
		}
	}

	for _, sn := range localSnaps {
		if sn.SideInfo.SnapID == "" {
			return fmt.Errorf("cannot create recovery system from provided snap that has no snap id: %q", sn.SideInfo.RealName)
		}
	}

	return nil
}

func offlineSnapInfo(sn *asserts.ModelSnap, rev snap.Revision, opts CreateRecoverySystemOptions) (*snap.Info, error) {
	index := -1
	for i, si := range opts.LocalSnaps {
		if sn.ID() == si.SideInfo.SnapID {
			index = i
			break
		}
	}
	if index == -1 {
		return nil, fmt.Errorf(
			"missing snap from local snaps provided for offline creation of recovery system: %q, rev %v", sn.Name, rev,
		)
	}

	localSnap := opts.LocalSnaps[index]

	if !rev.Unset() && rev != localSnap.SideInfo.Revision {
		return nil, fmt.Errorf(
			"snap %q does not match revision required by validation sets: %v != %v", localSnap.SideInfo.RealName, localSnap.SideInfo.Revision, rev,
		)
	}

	s := mylog.Check2(snapfile.Open(localSnap.Path))

	return snap.ReadInfoFromSnapFile(s, localSnap.SideInfo)
}

func snapNeedsInstall(st *state.State, name string, rev snap.Revision) (bool, error) {
	info := mylog.Check2(snapstate.CurrentInfo(st, name))

	if rev.Unset() {
		return false, nil
	}

	return rev != info.Revision, nil
}

func extractSnapSetupTaskIDs(tss []*state.TaskSet) ([]string, error) {
	var taskIDs []string
	for _, ts := range tss {
		found := false
		for _, t := range ts.Tasks() {
			if t.Has("snap-setup") {
				taskIDs = append(taskIDs, t.ID())
				found = true
				break
			}
		}

		if !found {
			return nil, errors.New("internal error: snap setup task missing from task set")
		}
	}
	return taskIDs, nil
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
