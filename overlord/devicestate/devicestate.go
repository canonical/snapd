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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
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
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

var (
	snapstateDownloadComponents                 = snapstate.DownloadComponents
	snapstateDownload                           = snapstate.Download
	snapstateUpdateOne                          = snapstate.UpdateOne
	snapstateInstallOne                         = snapstate.InstallOne
	snapstateStoreInstallGoal                   = snapstate.StoreInstallGoal
	snapstatePathInstallGoal                    = snapstate.PathInstallGoal
	snapstateStoreUpdateGoal                    = snapstate.StoreUpdateGoal
	snapstatePathUpdateGoal                     = snapstate.PathUpdateGoal
	snapstateInstallComponents                  = snapstate.InstallComponents
	snapstateInstallComponentPath               = snapstate.InstallComponentPath
	remodelChangeKind                           = swfeats.ChangeReg.Add("remodel")
	removeRecoverySystemChangeKind              = swfeats.ChangeReg.Add("remove-recovery-system")
	createRecoverySystemChangeKind              = swfeats.ChangeReg.Add("create-recovery-system")
	installStepFinishChangeKind                 = swfeats.ChangeReg.Add("install-step-finish")
	installStepSetupStorageEncryptionChangeKind = swfeats.ChangeReg.Add("install-step-setup-storage-encryption")
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

// findKnownRevisionOfModel returns the model assertion revision if any in the
// assertion database for the given model, otherwise it returns -1.
func findKnownRevisionOfModel(st *state.State, mod *asserts.Model) (modRevision int, err error) {
	a, err := assertstate.DB(st).Find(asserts.ModelType, map[string]string{
		"series":   release.Series,
		"brand-id": mod.BrandID(),
		"model":    mod.Model(),
	})
	if errors.Is(err, &asserts.NotFoundError{}) {
		return -1, nil
	}
	if err != nil {
		return 0, err
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

// CanManageRefreshes returns true if a snap entitled to setting the
// refresh-schedule to managed is installed in the system and the relevant
// interface is currently connected.
//
// TODO:
//   - Move the CanManageRefreshes code into the ifstate
//   - Look at the connections and find the connection for snapd-control
//     with the managed attribute
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
	// we know we always start with downloads (or prepare-snap tasks, in the
	// case of an offline remodel)
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
	new *asserts.Model

	oldSnap      string
	oldModelSnap *asserts.ModelSnap

	newSnap      string
	newModelSnap *asserts.ModelSnap
}

type remodeler struct {
	newModel        *asserts.Model
	offline         bool
	localSnaps      map[string]snapstate.PathSnap
	localComponents map[string]snapstate.PathComponent

	vsets      *snapasserts.ValidationSets
	tracker    *snap.SelfContainedSetPrereqTracker
	deviceCtx  snapstate.DeviceContext
	fromChange string
}

// remodelSnapTarget represents a snap that is part of the model that we are
// remodeling to.
type remodelSnapTarget struct {
	// name is the name of the snap.
	name string
	// channel is the channel that the snap should be installed from and track.
	channel string
	// newModelSnap is the model snap for this target. This might be nil for
	// either the snapd snap (which is implicitly in the model) or for the base
	// snap on UC16 models. Always check for nil before using.
	newModelSnap *asserts.ModelSnap
	// oldModelSnap is the corresponding model snap for the snap that this
	// target is replacing. This will be nil for non-essential snaps, and it
	// might be nil for the snapd snap (which is implicitly in the model) or for
	// the base snap on UC16 models. Always check for nil before using.
	oldModelSnap *asserts.ModelSnap
}

// canHaveUC18PinnedTrack returns whether the given model snap can have a pinned
// track. Only the kernel and gadget snaps from a UC18 model can have a pinned
// track. Note that this is different than the default-channel that is used for
// UC20+ models.
func canHaveUC18PinnedTrack(ms *asserts.ModelSnap) bool {
	return ms != nil && (ms.SnapType == "kernel" || ms.SnapType == "gadget")
}

// uc20Model returns true if the given model is a UC20+ model. UC20+ models can
// be identified by the presence of a grade in the model.
func uc20Model(m *asserts.Model) bool {
	return m.Grade() != asserts.ModelGradeUnset
}

type remodelAction int

const (
	remodelInvalidAction remodelAction = iota
	remodelNoAction
	remodelChannelSwitch
	remodelInstallAction
	remodelUpdateAction
	remodelAddComponentsAction
)

func (r *remodeler) maybeInstallOrUpdate(ctx context.Context, st *state.State, rt remodelSnapTarget) (remodelAction, []*state.TaskSet, error) {
	var requiredComponents, optionalComponents []string
	if ms := rt.newModelSnap; ms != nil {
		for comp, mc := range ms.Components {
			switch mc.Presence {
			case "required":
				requiredComponents = append(requiredComponents, comp)
			case "optional":
				optionalComponents = append(optionalComponents, comp)
			}
		}
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, rt.name, &snapst); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return 0, nil, err
		}

		// if the snap isn't already installed and it isn't required, then we
		// can skip installing it. anything that has a nil model snap is
		// implicitly required (either snapd or a UC16 base)
		if rt.newModelSnap != nil && rt.newModelSnap.Presence != "required" {
			return remodelNoAction, nil, nil
		}

		goal, err := r.installGoal(rt, requiredComponents)
		if err != nil {
			return 0, nil, err
		}

		_, ts, err := snapstateInstallOne(ctx, st, goal, snapstate.Options{
			DeviceCtx:     r.deviceCtx,
			FromChange:    r.fromChange,
			PrereqTracker: r.tracker,
			Flags:         snapstate.Flags{NoReRefresh: true, Required: true},
		})
		if err != nil {
			return 0, nil, err
		}

		return remodelInstallAction, []*state.TaskSet{ts}, nil
	}

	// on UC20+ models, we look at the currently tracked channel to determine if
	// we are switching the channel. on UC18 models, we compare the pinned track
	// on the new model snap with the pinned track on the old model snap. note
	// that only the kernel and gadget snaps can have a pinned track on UC18
	// models.
	var currentChannelOrTrack string
	if uc20Model(r.newModel) {
		currentChannelOrTrack = snapst.TrackingChannel
	} else if canHaveUC18PinnedTrack(rt.oldModelSnap) {
		currentChannelOrTrack = rt.oldModelSnap.PinnedTrack
	}
	needsChannelChange := rt.channel != "" && rt.channel != currentChannelOrTrack && !snapst.Current.Local()

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return 0, nil, err
	}

	constraints, err := r.vsets.Presence(naming.Snap(rt.name))
	if err != nil {
		return 0, nil, err
	}

	if !constraints.Revision.Unset() && snapst.Current.Local() {
		return 0, nil, errors.New("cannot determine if unasserted snap revision matches required revision")
	}

	// we need to change the revision if either the incoming model's validation
	// sets require a specific revision that we don't have installed, or if the
	// current revision doesn't support the components that we need.
	needsRevisionChange := (!constraints.Revision.Unset() && constraints.Revision != snapst.Current) || !revisionSupportsComponents(currentInfo, requiredComponents)

	needsComponentChanges, requiredOptionalComponents := checkForComponentRemodelingChanges(
		rt, snapst, requiredComponents, optionalComponents, constraints, needsRevisionChange,
	)

	// if we're not going to swap snaps, then we must require that any optional
	// components that are already installed at an invalid revision are
	// updated/provided locally.
	requiredComponents = append(requiredComponents, requiredOptionalComponents...)

	// TODO: we don't properly handle snaps (and now components) that are
	// invalid in the incoming model and required by the previous model. this
	// would require removing things during a remodel, which isn't something we
	// do at the moment. afaict, it is impossible to remodel from a model that
	// requires a snap that is invalid in the incoming model.

	switch {
	case needsRevisionChange || needsChannelChange:
		if r.shouldSwitchWithoutRefresh(rt, needsRevisionChange) && !needsComponentChanges {
			ts, err := snapstate.Switch(st, rt.name, &snapstate.RevisionOptions{
				Channel: rt.channel,
			}, r.tracker)
			if err != nil {
				return 0, nil, err
			}

			return remodelChannelSwitch, []*state.TaskSet{ts}, nil
		}

		// right now, we don't properly handle switching a channel and
		// installing components at the same time. in the meantime, we can use
		// snapstate.UpdateOne to add additional components and switch the
		// channel for us. this method is suboptimal, since we're creating tasks
		// for essentially re-installing the snap.
		//
		// this also will not work well for offline remodeling, since it
		// prevents us from using a combination of locally provided components
		// and an already installed snap. for that case,
		// snapstate.InstallComponents would need to support switching channels
		// at the same time as installing components.
		goal, err := r.updateGoal(st, rt, requiredComponents, constraints)
		if err != nil {
			return 0, nil, err
		}

		ts, err := snapstateUpdateOne(ctx, st, goal, nil, snapstate.Options{
			DeviceCtx:     r.deviceCtx,
			FromChange:    r.fromChange,
			PrereqTracker: r.tracker,
			Flags:         snapstate.Flags{NoReRefresh: true},
		})
		if err != nil {
			return 0, nil, err
		}

		// if there are any local modfifications, we know that we're doing more
		// than a channel switch
		if ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge) != nil {
			return remodelUpdateAction, []*state.TaskSet{ts}, nil
		}

		return remodelChannelSwitch, []*state.TaskSet{ts}, nil
	case needsComponentChanges:
		tss, err := r.installComponents(ctx, st, currentInfo, requiredComponents)
		if err != nil {
			return 0, nil, err
		}
		return remodelAddComponentsAction, tss, nil
	default:
		// nothing to do but add the snap to the prereq tracker
		r.tracker.Add(currentInfo)
		return remodelNoAction, nil, nil
	}
}

// checkForComponentRemodelingChanges determines if we need to make any changes to the
// existing state of the components for the given remodel target. Additionally,
// if it is determined that we can use the current snap's revision, then any
// already-installed optional components that must have their revision changed
// to fulfill the validation set constraints are returned.
func checkForComponentRemodelingChanges(
	rt remodelSnapTarget,
	snapst snapstate.SnapState,
	requiredComponents []string,
	optionalComponents []string,
	constraints snapasserts.SnapPresenceConstraints,
	snapNeedsRevisionChange bool,
) (needsComponentChanges bool, requiredOptionalComponents []string) {
	// check if any components are either missing, or installed at the wrong
	// revision. note that we will only explicitly handle these needed changes
	// if the snap itself, and its channel, are already valid in the incoming
	// model
	for _, c := range requiredComponents {
		csi := snapst.CurrentComponentSideInfo(naming.NewComponentRef(rt.name, c))
		if csi == nil {
			needsComponentChanges = true
			break
		}

		compConstraints := constraints.Component(c)
		if !compConstraints.Revision.Unset() && compConstraints.Revision != csi.Revision {
			needsComponentChanges = true
			break
		}
	}

	// if we're not changing the revision, then we have to check if any of the
	// model's optional components are installed and make sure that they are at
	// the correct revision. if they aren't then we'll either attempt to update
	// them from the store or they must come from a given file.
	if !snapNeedsRevisionChange {
		requiredOptionalComponents = make([]string, 0, len(optionalComponents))
		for _, c := range optionalComponents {
			csi := snapst.CurrentComponentSideInfo(naming.NewComponentRef(rt.name, c))
			if csi == nil {
				continue
			}

			compConstraints := constraints.Component(c)
			if !compConstraints.Revision.Unset() && compConstraints.Revision != csi.Revision {
				needsComponentChanges = true
				requiredOptionalComponents = append(requiredOptionalComponents, c)
			}
		}
	}
	return needsComponentChanges, requiredOptionalComponents
}

func (r *remodeler) shouldSwitchWithoutRefresh(rt remodelSnapTarget, needsRevisionChange bool) bool {
	if !r.offline {
		return false
	}

	if needsRevisionChange {
		return false
	}

	// if we have a local container for this snap, then we should use that in
	// addition to switching the tracked channel
	if _, ok := r.localSnaps[rt.name]; ok {
		return false
	}

	return true
}

func revisionSupportsComponents(info *snap.Info, components []string) bool {
	for _, c := range components {
		if _, ok := info.Components[c]; !ok {
			return false
		}
	}
	return true
}

func (r *remodeler) installGoal(sn remodelSnapTarget, components []string) (snapstate.InstallGoal, error) {
	if r.offline {
		ls, ok := r.localSnaps[sn.name]
		if !ok {
			return nil, fmt.Errorf("no snap file provided for %q", sn.name)
		}

		comps := make([]snapstate.PathComponent, 0, len(components))
		for _, c := range components {
			cref := naming.NewComponentRef(sn.name, c)
			lc, ok := r.localComponents[cref.String()]
			if !ok {
				return nil, fmt.Errorf("cannot find locally provided component: %q", cref)
			}

			comps = append(comps, lc)
		}

		opts := snapstate.RevisionOptions{
			Channel:        sn.channel,
			ValidationSets: r.vsets,
		}

		// TODO: snapstate for by-path installs doesn't verify validation sets.
		// decide if we want to manually verify the given rules here or not.

		return snapstatePathInstallGoal(snapstate.PathSnap{
			Path:       ls.Path,
			SideInfo:   ls.SideInfo,
			RevOpts:    opts,
			Components: comps,
		}), nil
	}

	return snapstateStoreInstallGoal(snapstate.StoreSnap{
		InstanceName: sn.name,
		Components:   components,
		RevOpts: snapstate.RevisionOptions{
			Channel:        sn.channel,
			ValidationSets: r.vsets,
		},
	}), nil
}

// installedRevisionUpdateGoal returns an update goal which will install a snap
// revision that was previously installed on the system and still in the
// sequence. We use a [snapstate.PathUpdateGoal] to enable this.
func (r *remodeler) installedRevisionUpdateGoal(
	st *state.State,
	sn remodelSnapTarget,
	components []string,
	constraints snapasserts.SnapPresenceConstraints,
) (snapstate.UpdateGoal, error) {
	if constraints.Revision.Unset() {
		return nil, errors.New("internal error: falling back to a previous revision requires that we have a specific revision to pick")
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, sn.name, &snapst); err != nil {
		return nil, err
	}

	index := snapst.LastIndex(constraints.Revision)
	if index == -1 {
		return nil, fmt.Errorf("installed snap %q does not have the required revision in its sequence to be used for offline remodel: %s", sn.name, constraints.Revision)
	}

	ss := snapst.Sequence.Revisions[index]
	comps := make([]snapstate.PathComponent, 0, len(ss.Components))
	for _, c := range components {
		cref := naming.NewComponentRef(snap.InstanceSnap(sn.name), c)
		cs := ss.FindComponent(cref)
		if cs == nil {
			return nil, fmt.Errorf("cannot find required component in set of already installed components: %s", cref)
		}

		compConstraints := constraints.Component(cs.SideInfo.Component.ComponentName)
		if !compConstraints.Revision.Unset() && compConstraints.Revision != cs.SideInfo.Revision {
			return nil, fmt.Errorf("cannot fall back to component %q with revision %s, required revision is %s", cs.SideInfo.Component, cs.SideInfo.Revision, compConstraints.Revision)
		}

		cpi := snap.MinimalComponentContainerPlaceInfo(
			cs.SideInfo.Component.ComponentName,
			cs.SideInfo.Revision,
			snapst.InstanceName(),
		)

		comps = append(comps, snapstate.PathComponent{
			SideInfo: cs.SideInfo,
			Path:     cpi.MountFile(),
		})
	}

	sideInfo := *ss.Snap

	// despite swapping back to an old revision in the sequence, we still might
	// need to swap to a new channel to track.
	if sn.channel != "" {
		sideInfo.Channel = sn.channel
	}

	return snapstatePathUpdateGoal(snapstate.PathSnap{
		InstanceName: sn.name,
		Path:         snap.MountFile(sn.name, constraints.Revision),
		SideInfo:     &sideInfo,
		Components:   comps,
		RevOpts: snapstate.RevisionOptions{
			Channel:        sn.channel,
			ValidationSets: r.vsets,
			Revision:       constraints.Revision,
		},
	}), nil
}

func (r *remodeler) updateGoal(st *state.State, sn remodelSnapTarget, components []string, constraints snapasserts.SnapPresenceConstraints) (snapstate.UpdateGoal, error) {
	if r.offline {
		ls, ok := r.localSnaps[sn.name]
		if !ok {
			// this attempts to create a snapstate.StoreUpdateGoal that will
			// switch back to a previously installed snap revision that is still
			// in the sequence
			g, err := r.installedRevisionUpdateGoal(st, sn, components, constraints)
			if err != nil {
				return nil, err
			}
			return g, nil
		}

		// we assume that all of the component revisions are valid with the
		// given snap revision. the code in daemon that calls Remodel verifies
		// this against the assertions db, and the task handlers in snapstate
		// also double check this while installing the snap/components.
		comps := make([]snapstate.PathComponent, 0, len(components))
		for _, c := range components {
			cref := naming.NewComponentRef(sn.name, c)

			lc, ok := r.localComponents[cref.String()]
			if !ok {
				return nil, fmt.Errorf("cannot find locally provided component: %q", cref)
			}

			comps = append(comps, lc)
		}

		opts := snapstate.RevisionOptions{
			Channel:        sn.channel,
			ValidationSets: r.vsets,
		}

		// TODO: snapstate for by-path installs doesn't verify validation sets.
		// decide if we want to manually verify the given rules here or not.

		return snapstatePathUpdateGoal(snapstate.PathSnap{
			Path:       ls.Path,
			SideInfo:   ls.SideInfo,
			RevOpts:    opts,
			Components: comps,
		}), nil
	}

	return snapstateStoreUpdateGoal(snapstate.StoreUpdate{
		InstanceName: sn.name,
		RevOpts: snapstate.RevisionOptions{
			Channel:        sn.channel,
			ValidationSets: r.vsets,
		},
		// components will be the full list of components needed by the new
		// model, and it might already contain any of the components that are
		// already installed. the snapstate code handles this case correctly.
		AdditionalComponents: components,
	}), nil
}

func (r *remodeler) installComponents(ctx context.Context, st *state.State, info *snap.Info, components []string) ([]*state.TaskSet, error) {
	r.tracker.Add(info)

	if r.offline {
		var tss []*state.TaskSet
		for _, c := range components {
			ref := naming.NewComponentRef(info.SnapName(), c)

			lc, ok := r.localComponents[ref.String()]
			if !ok {
				return nil, fmt.Errorf("cannot find locally provided component: %q", ref)
			}

			ts, err := snapstateInstallComponentPath(st, lc.SideInfo, info, lc.Path, snapstate.Options{
				DeviceCtx:     r.deviceCtx,
				FromChange:    r.fromChange,
				PrereqTracker: r.tracker,
			})
			if err != nil {
				return nil, err
			}
			tss = append(tss, ts)

			// TODO: snapstate for by-path installs doesn't verify validation sets.
			// decide if we want to manually verify the given rules here or not.
		}
		return tss, nil
	}

	return snapstateInstallComponents(ctx, st, components, info, r.vsets, snapstate.Options{
		DeviceCtx:     r.deviceCtx,
		FromChange:    r.fromChange,
		PrereqTracker: r.tracker,
	})
}

func remodelEssentialSnapTasks(
	ctx context.Context,
	st *state.State,
	rm remodeler,
	ms modelSnapsForRemodel,
) ([]*state.TaskSet, error) {
	newModelSnapChannel, err := modelSnapChannelFromDefaultOrPinnedTrack(ms.new, ms.newModelSnap)
	if err != nil {
		return nil, err
	}

	rt := remodelSnapTarget{
		name:         ms.newSnap,
		channel:      newModelSnapChannel,
		newModelSnap: ms.newModelSnap,
		oldModelSnap: ms.oldModelSnap,
	}

	logger.Debugf("creating remodel tasks for essential snap %s", ms.newSnap)
	action, tss, err := rm.maybeInstallOrUpdate(ctx, st, rt)
	if err != nil {
		return nil, err
	}

	// if we're not swapping to a new essential snap, then it should already be
	// fully available during the remodel.
	if ms.newSnap == ms.oldSnap {
		return tss, nil
	}

	// below covers some edge cases for remodeling when the current system
	// already has some of the new model's essential snaps installed.
	//
	// note that it may seem that we are unnecessarily handling some cases for
	// kernels and gadgets, which usually are exclusive on a system. however,
	// since we do not remove snaps during a remodel, a system might have
	// multiple gadget or kernel snaps installed from a previous remodel. in
	// those cases, we will need to create the tasks to make them available
	// during the remodel, since they won't have been boot participants until
	// now.

	// when we're not modifying anything to do with the snap itself, we need to
	// create some tasks to ensure that the essential snap is available during
	// the remodel. this is done in the link-snap task, which checks to see if
	// the snap is a boot participant.
	switchEssentialTasks := func(name, fromChange string) (*state.TaskSet, error) {
		if ms.newModelSnap != nil && ms.newModelSnap.SnapType == "gadget" {
			return snapstate.SwitchToNewGadget(st, name, fromChange)
		}
		return snapstate.LinkNewBaseOrKernel(st, name, fromChange)
	}

	// as a bit of a special case, we support adding the needed tasks that make
	// the snap available during the remodel to an existing task set. this is
	// used when we create a task set that only changes the snap's channel.
	appendSwitchEssentialTasks := func(tss []*state.TaskSet) (*state.TaskSet, error) {
		if len(tss) != 1 {
			return nil, errors.New("internal error: a channel switch should only have one task set")
		}

		if ms.newModelSnap != nil && ms.newModelSnap.SnapType == "gadget" {
			return snapstate.AddGadgetAssetsTasks(st, tss[0])
		}
		return snapstate.AddLinkNewBaseOrKernel(st, tss[0])
	}

	switch action {
	case remodelUpdateAction, remodelInstallAction:
		// if we're updating or installing a new essential snap, everything will
		// already be handled
		return tss, nil
	case remodelNoAction, remodelAddComponentsAction:
		ts, err := switchEssentialTasks(ms.newSnap, rm.fromChange)
		if err != nil {
			return nil, err
		}
		return append(tss, ts), nil
	case remodelChannelSwitch:
		ts, err := appendSwitchEssentialTasks(tss)
		if err != nil {
			return nil, err
		}
		return []*state.TaskSet{ts}, nil
	default:
		return nil, fmt.Errorf("internal error: unhandled remodel action: %d", action)
	}
}

// tasksForEssentialSnap returns tasks for essential snaps (actually,
// except for the snapd snap).
func tasksForEssentialSnap(
	ctx context.Context,
	st *state.State,
	snapType string,
	current, new *asserts.Model,
	rm remodeler,
) ([]*state.TaskSet, error) {
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
		oldSnap:      currentSnap,
		oldModelSnap: currentModelSnap,
		new:          new,
		newSnap:      newSnap,
		newModelSnap: newModelSnap,
	}
	return remodelEssentialSnapTasks(ctx, st, rm, ms)
}

func remodelSnapdSnapTasks(ctx context.Context, st *state.State, rm remodeler) ([]*state.TaskSet, error) {
	// First check if snapd snap is installed at all (might be the case
	// for uc16, which happens for some tests).
	var ss snapstate.SnapState
	if err := snapstate.Get(st, "snapd", &ss); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, nil
		}
		return nil, err
	}

	// Implicit new channel if snapd is not explicitly in the model
	newSnapdChannel := "latest/stable"
	essentialSnaps := rm.newModel.EssentialSnaps()
	if essentialSnaps[0].SnapType == "snapd" {
		// snapd can be specified explicitly in the model (UC20+)
		newSnapdChannel = essentialSnaps[0].DefaultChannel
	}

	_, tss, err := rm.maybeInstallOrUpdate(ctx, st, remodelSnapTarget{
		name:    "snapd",
		channel: newSnapdChannel,
	})
	if err != nil {
		return nil, err
	}
	return tss, nil
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
	deviceCtx snapstate.DeviceContext, fromChange string, opts RemodelOptions) ([]*state.TaskSet, error) {

	logger.Debugf("creating remodeling tasks")

	vsets, err := verifyModelValidationSets(st, new, opts.Offline, deviceCtx)
	if err != nil {
		return nil, err
	}

	// If local snaps are provided, all needed snaps must be locally
	// provided. We check this flag whenever a snap installation/update is
	// found needed for the remodel.
	rm := remodeler{
		newModel:        new,
		offline:         opts.Offline,
		vsets:           vsets,
		tracker:         snap.NewSelfContainedSetPrereqTracker(),
		deviceCtx:       deviceCtx,
		fromChange:      fromChange,
		localSnaps:      make(map[string]snapstate.PathSnap, len(opts.LocalSnaps)),
		localComponents: make(map[string]snapstate.PathComponent, len(opts.LocalComponents)),
	}

	for _, ls := range opts.LocalSnaps {
		rm.localSnaps[ls.SideInfo.RealName] = snapstate.PathSnap{
			Path:     ls.Path,
			SideInfo: ls.SideInfo,
		}
	}

	for _, lc := range opts.LocalComponents {
		rm.localComponents[lc.SideInfo.Component.String()] = lc
	}

	// First handle snapd as a special case
	tss, err := remodelSnapdSnapTasks(ctx, st, rm)
	if err != nil {
		return nil, err
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
		sets, err := tasksForEssentialSnap(ctx, st, modelSnap.SnapType, current, new, rm)
		if err != nil {
			return nil, err
		}
		tss = append(tss, sets...)
	}

	// if base is not set, then core will not be returned in the list of snaps
	// returned by new.EssentialSnaps(). since we know that we are remodeling
	// from a core-based system to a core-based system, then the core snap must
	// be installed. thus, we can safely add it to the prereq tracker. note that
	// moving from a UC16 model to a newer model is not supported.
	if new.Base() == "" {
		currentBase, err := snapstate.CurrentInfo(st, "core")
		if err != nil {
			return nil, err
		}
		rm.tracker.Add(currentBase)
	}

	// sort the snaps so that we collect the task sets for base snaps first, and
	// then the rest. this prevents a later issue where we attempt to install a
	// snap, but the base is not yet installed.
	snapsWithoutEssential := sortNonEssentialRemodelTaskSetsBasesFirst(new.SnapsWithoutEssential())

	// go through all the model snaps, see if there are new required snaps
	// or a track for existing ones needs to be updated
	for _, modelSnap := range snapsWithoutEssential {
		logger.Debugf("adding remodel tasks for non-essential snap %s", modelSnap.Name)

		// default channel can be set only in UC20 models
		newModelSnapChannel, err := modelSnapChannelFromDefaultOrPinnedTrack(new, modelSnap)
		if err != nil {
			return nil, err
		}

		_, sets, err := rm.maybeInstallOrUpdate(ctx, st, remodelSnapTarget{
			name:         modelSnap.SnapName(),
			channel:      newModelSnapChannel,
			newModelSnap: modelSnap,
		})
		if err != nil {
			return nil, err
		}
		tss = append(tss, sets...)
	}

	if err := checkRequiredGadgetMatchesModelBase(new, rm.tracker); err != nil {
		return nil, err
	}

	warnings, errs := rm.tracker.Check()
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
	hasSystemSeed, err := checkForSystemSeed(st, deviceCtx)
	if err != nil {
		return nil, fmt.Errorf("cannot find ubuntu seed role: %w", err)
	}

	recoverySetupTaskID := ""
	if new.Grade() != asserts.ModelGradeUnset && hasSystemSeed {
		// create a recovery when remodeling to a UC20 system, actual
		// policy for possible remodels has already been verified by the
		// caller
		labelBase := timeNow().Format("20060102")
		label, err := pickRecoverySystemLabel(labelBase)
		if err != nil {
			return nil, fmt.Errorf("cannot select non-conflicting label for recovery system %q: %v", labelBase, err)
		}
		// we don't pass in the list of local snaps here because they are
		// already represented by snapSetupTasks

		snapsupTaskIDs, compsupTaskIDs, err := setupTaskIDsForCreatingRecoverySystem(tss)
		if err != nil {
			return nil, err
		}

		createRecoveryTasks, err := createRecoverySystemTasks(st, label, snapsupTaskIDs, compsupTaskIDs, CreateRecoverySystemOptions{
			TestSystem: true,
		})
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

	// Ensure correct restart boundaries are set on the new task-set.
	if err := snapstate.SetEssentialSnapsRestartBoundaries(st, deviceCtx, tss); err != nil {
		return nil, err
	}
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
	vSets, err := assertstate.ValidationSetsFromModel(st, newModel, assertstate.FetchValidationSetsOptions{
		Offline: offline,
	}, deviceCtx)
	if err != nil {
		return nil, err
	}

	if err := checkForInvalidSnapsInModel(newModel, vSets); err != nil {
		return nil, err
	}

	if err := checkForRequiredSnapsNotRequiredInModel(newModel, vSets); err != nil {
		return nil, err
	}

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

	// TODO:COMPS: consider relationship with required components here

	return nil
}

func checkForInvalidSnapsInModel(model *asserts.Model, vSets *snapasserts.ValidationSets) error {
	if len(vSets.Keys()) == 0 {
		return nil
	}

	for _, sn := range model.AllSnaps() {
		pres, err := vSets.Presence(sn)
		if err != nil {
			return err
		}

		if pres.Presence == asserts.PresenceInvalid {
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

	gadgetData, err := CurrentGadgetData(st, deviceCtx)
	if err != nil {
		return false, fmt.Errorf("cannot get gadget data: %w", err)
	}

	return gadgetData.Info.HasRole(gadget.SystemSeed), nil
}

// RemodelOptions are options for Remodel.
type RemodelOptions struct {
	// Offline is true if the remodel should be done without reaching out to the
	// store. Any snaps needed for the remodel, that are not already installed,
	// should be provided via the parameters to Remodel. Snaps that are already
	// installed will be used if they match the revisions that are required by
	// the model.
	Offline         bool
	LocalSnaps      []snapstate.PathSnap
	LocalComponents []snapstate.PathComponent
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
func Remodel(st *state.State, new *asserts.Model, opts RemodelOptions) (*state.Change, error) {
	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot remodel until fully seeded")
	}

	if !opts.Offline && (len(opts.LocalSnaps) > 0 || len(opts.LocalComponents) > 0) {
		return nil, errors.New("cannot do an online remodel with provided local snaps or components")
	}

	for _, ls := range opts.LocalSnaps {
		if ls.Components != nil || ls.InstanceName != "" || ls.RevOpts != (snapstate.RevisionOptions{}) {
			return nil, errors.New("internal error: locally provided snaps must only provide path and side info")
		}
	}

	current, err := findModel(st)
	if err != nil {
		return nil, err
	}

	prevRev, err := findKnownRevisionOfModel(st, new)
	if err != nil {
		return nil, err
	}
	if new.Revision() < prevRev {
		return nil, fmt.Errorf("cannot remodel to older revision %d of model %s/%s than last revision %d known to the device", new.Revision(), new.BrandID(), new.Model(), prevRev)
	}

	// TODO: we need dedicated assertion language to permit for
	// model transitions before we allow cross vault
	// transitions.

	remodelKind := ClassifyRemodel(current, new)

	if _, err := findSerial(st, nil); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}

		if opts.Offline && remodelKind == UpdateRemodel {
			// it is allowed to remodel without serial for
			// offline remodels that are update only
		} else {
			return nil, fmt.Errorf("cannot remodel without a serial")
		}
	}

	if current.Series() != new.Series() {
		return nil, fmt.Errorf("cannot remodel to different series yet")
	}

	devCtx, err := DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot get device context: %v", err)
	}

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
		err := sto.EnsureDeviceSession()
		st.Lock()
		if err != nil {
			return nil, fmt.Errorf("cannot get a store session based on the new model assertion: %v", err)
		}
		fallthrough
	case UpdateRemodel:
		// TODO: make this case follow the same pattern as ReregRemodel, where
		// we call remodelTasks from inside another task, so that the tasks for
		// the remodel are added to an existing and running change. this will
		// allow us to avoid things like calling snapstate.CheckChangeConflictRunExclusively again.
		var err error
		tss, err = remodelTasks(context.TODO(), st, current, new, remodCtx, "", opts)
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

	// check for exclusive changes again since we released the lock
	if err := snapstate.CheckChangeConflictRunExclusively(st, "remodel"); err != nil {
		return nil, err
	}

	var msg string
	if current.BrandID() == new.BrandID() && current.Model() == new.Model() {
		msg = fmt.Sprintf(i18n.G("Refresh model assertion from revision %v to %v"), current.Revision(), new.Revision())
	} else {
		msg = fmt.Sprintf(i18n.G("Remodel device to %v/%v (%v)"), new.BrandID(), new.Model(), new.Revision())
	}

	chg := st.NewChange(remodelChangeKind, msg)
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
	SnapSetupTasks []string `json:"snap-setup-tasks,omitempty"`
	// LocalSnaps is a list of snaps that should be used to create the recovery
	// system.
	LocalSnaps []snapstate.PathSnap `json:"local-snaps,omitempty"`
	// ComponentSetupTasks is a list of task IDs that carry component setup
	// information. Tasks could come from a remodel, or from downloading
	// components that were required by a validation set.
	ComponentSetupTasks []string `json:"component-setup-tasks,omitempty"`
	// LocalComponents is a list of components that should be used to create the
	// recovery system.
	LocalComponents []snapstate.PathComponent `json:"local-components,omitempty"`
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

func createRecoverySystemTasks(st *state.State, label string, snapSetupTasks, compSetupTasks []string, opts CreateRecoverySystemOptions) (*state.TaskSet, error) {
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
		SnapSetupTasks:      snapSetupTasks,
		ComponentSetupTasks: compSetupTasks,
		LocalSnaps:          opts.LocalSnaps,
		LocalComponents:     opts.LocalComponents,
		TestSystem:          opts.TestSystem,
		MarkDefault:         opts.MarkDefault,
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
	// will be used to create a recovery system or remodel the system.
	SideInfo *snap.SideInfo

	// Path is the path on disk to a snap that will be used to create a recovery
	// system or remodel the system.
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
	LocalSnaps []snapstate.PathSnap

	// LocalComponents is an optional list of components that will be used to
	// create the new recovery system. If provided, this list must contain any
	// component that is not already installed that will be needed by the new
	// recovery system.
	LocalComponents []snapstate.PathComponent

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
	if err := snapstate.CheckChangeConflictRunExclusively(st, "remove-recovery-system"); err != nil {
		return nil, err
	}

	recoverySystemsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems")
	exists, _, err := osutil.DirExists(filepath.Join(recoverySystemsDir, label))
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("%q not found: %w", label, ErrNoRecoverySystem)
	}

	chg := st.NewChange(removeRecoverySystemChangeKind, fmt.Sprintf("Remove recovery system with label %q", label))

	removeTS, err := removeRecoverySystemTasks(st, label)
	if err != nil {
		return nil, err
	}

	chg.AddAll(removeTS)

	return chg, nil
}

func checkForRequiredSnapsNotPresentInModel(model *asserts.Model, vSets *snapasserts.ValidationSets) error {
	snapsInModel := make(map[string]bool, len(model.AllSnaps()))
	for _, sn := range model.AllSnaps() {
		snapsInModel[sn.SnapName()] = true
	}

	for _, sn := range vSets.RequiredSnaps() {
		if !snapsInModel[sn] {
			return fmt.Errorf("missing required snap in model: %s", sn)
		}
	}

	return nil
}

// CreateRecoverySystem creates a new recovery system with the given label. See
// CreateRecoverySystemOptions for details on the options that can be provided.
func CreateRecoverySystem(st *state.State, label string, opts CreateRecoverySystemOptions) (*state.Change, error) {
	if err := snapstate.CheckChangeConflictRunExclusively(st, "create-recovery-system"); err != nil {
		return nil, err
	}

	if !opts.Offline && (len(opts.LocalSnaps) > 0 || len(opts.LocalComponents) > 0) {
		return nil, errors.New("local snaps/components cannot be provided when creating a recovery system online")
	}

	var seeded bool
	err := st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !seeded {
		return nil, fmt.Errorf("cannot create new recovery systems until fully seeded")
	}

	model, err := findModel(st)
	if err != nil {
		return nil, err
	}

	valsets, err := assertstate.TrackedEnforcedValidationSetsForModel(st, model)
	if err != nil {
		return nil, err
	}

	for _, vs := range opts.ValidationSets {
		valsets.Add(vs)
	}

	if err := valsets.Conflict(); err != nil {
		return nil, err
	}

	// TODO: this restriction should be lifted eventually (in the case that we
	// have a dangerous model), and we should fall back to using snap names in
	// places that IDs are used
	if err := checkForSnapIDs(model, opts.LocalSnaps); err != nil {
		return nil, err
	}

	// check that all snaps from the model are valid in the validation sets
	if err := checkForInvalidSnapsInModel(model, valsets); err != nil {
		return nil, err
	}

	// the task that creates the recovery system doesn't know anything about
	// validation sets, so we cannot create systems with snaps that are not in
	// the model.
	if err := checkForRequiredSnapsNotPresentInModel(model, valsets); err != nil {
		return nil, err
	}

	tracker := snap.NewSelfContainedSetPrereqTracker()

	validRevision := func(current snap.Revision, constraints snapasserts.PresenceConstraint) bool {
		return constraints.Revision.Unset() || current == constraints.Revision
	}

	usedLocalSnaps := make([]snapstate.PathSnap, 0, len(opts.LocalSnaps))
	usedLocalComps := make([]snapstate.PathComponent, 0, len(opts.LocalComponents))

	var downloadTSS []*state.TaskSet
	for _, sn := range model.AllSnaps() {
		constraints, err := valsets.Presence(sn)
		if err != nil {
			return nil, err
		}

		installed, currentRevision, err := installedSnapRevision(st, sn.Name)
		if err != nil {
			return nil, err
		}

		// we must consider the snap as required to create this recovery system
		// in a few cases:
		// * the snap is required by the model
		// * the snap is required by the validation sets
		// * the snap is optional in the model but already installed. we
		//   consider the snap required in this case because the task handler for
		//   create-recovery-system will use any optional snaps that are
		//   installed, regardless of the snap's revision. requiring this snap
		//   ensures that we get the correct revision with respect to any given
		//   validation sets.
		//
		// TODO: consider making create-recovery-system aware of validation
		// sets, allowing us to avoid requiring optional but installed snaps
		required := constraints.Presence == asserts.PresenceRequired || sn.Presence == "required" || installed
		if !required {
			continue
		}

		installedSnapValid := installed && validRevision(currentRevision, constraints.PresenceConstraint)

		// keep track of the components that need to either be given to us or
		// downloaded
		requiredComponents := make([]string, 0, len(sn.Components))

		for name, comp := range sn.Components {
			compInstalled, currentCompRevision, err := installedComponentRevision(st, sn.Name, name)
			if err != nil {
				return nil, err
			}

			compConstraints := constraints.Component(name)

			// we must consider the component as required to create this
			// recovery system in a few cases:
			// * the component is required by the model
			// * the component is required by the validation sets
			// * the component is optional in the model but already installed.
			//   this is for the same reasons that we must consider the same case
			//   for snaps above.
			//
			// TODO: consider making create-recovery-system aware of validation
			// sets, allowing us to avoid requiring optional but installed components
			required := comp.Presence == "required" || compConstraints.Presence == asserts.PresenceRequired || compInstalled
			if !required {
				continue
			}

			// we must either download or have local components for all required
			// components that are either not installed, installed at an invalid
			// revision, or installed alongside a different snap revision. the
			// last condition could be eliminated by creating a task that only
			// downloads the missing snap-resource-pair assertion.
			if compInstalled && validRevision(currentCompRevision, compConstraints) && installedSnapValid {
				continue
			}

			requiredComponents = append(requiredComponents, name)
		}

		switch {
		case opts.Offline:
			// offline case, everything must either already be installed or
			// provided via the local snaps/components.

			// even if the installed snap revision might be valid, we still
			// should attempt to use the one that is provided by the caller.
			//
			// this matches what create-recovery-system does. it first checks
			// for provided local snaps, and then falls back to the installed
			// one.
			info, localSnap, err := offlineSnapInfo(sn, constraints.Revision, opts)
			if err != nil {
				if !errors.Is(err, errMissingLocalSnap) || !installedSnapValid {
					return nil, err
				}

				info, err = snapstate.CurrentInfo(st, sn.Name)
				if err != nil {
					return nil, err
				}
			} else {
				usedLocalSnaps = append(usedLocalSnaps, localSnap)
			}
			tracker.Add(info)

			for comp := range sn.Components {
				cref := naming.NewComponentRef(sn.Name, comp)
				rev := constraints.Component(comp).Revision

				localComp, err := offlineComponentInfo(cref, rev, opts.LocalComponents)
				if err != nil {
					// we only care if the component is present if it needs to
					// be provided.
					if strutil.ListContains(requiredComponents, comp) {
						return nil, err
					}
				} else {
					usedLocalComps = append(usedLocalComps, localComp)
				}
			}
		case installedSnapValid:
			// online case, but the currently installed snap revision is valid
			// in the given validation sets.

			info, err := snapstate.CurrentInfo(st, sn.Name)
			if err != nil {
				return nil, err
			}
			tracker.Add(info)

			if len(requiredComponents) > 0 {
				// TODO: download somewhere other than the default snap blob dir.
				ts, err := snapstateDownloadComponents(context.TODO(), st, sn.Name, requiredComponents, dirs.SnapBlobDir, snapstate.RevisionOptions{
					Channel:        sn.DefaultChannel,
					ValidationSets: valsets,
					Revision:       info.Revision,
				}, snapstate.Options{
					PrereqTracker: tracker,
				})
				if err != nil {
					return nil, err
				}
				downloadTSS = append(downloadTSS, ts)
			}
		default:
			// TODO: this respects the passed in validation sets, but does not
			// currently respect refresh-control style of constraining snap
			// revisions.
			//
			// TODO: download somewhere other than the default snap blob dir.
			ts, _, err := snapstateDownload(context.TODO(), st, sn.Name, requiredComponents, dirs.SnapBlobDir, snapstate.RevisionOptions{
				Channel:        sn.DefaultChannel,
				ValidationSets: valsets,
			}, snapstate.Options{
				PrereqTracker: tracker,
			})
			if err != nil {
				return nil, err
			}
			downloadTSS = append(downloadTSS, ts)

			// if we go in this branch, then we'll handle downloading snaps and
			// components at the same time.
			continue
		}
	}

	warnings, errs := tracker.Check()
	for _, w := range warnings {
		logger.Noticef("create recovery system prerequisites warning: %v", w)
	}

	if len(errs) > 0 {
		var builder strings.Builder
		builder.WriteString("cannot create recovery system from model that is not self-contained:")

		for _, err := range errs {
			builder.WriteString("\n  - ")
			builder.WriteString(err.Error())
		}

		return nil, errors.New(builder.String())
	}

	snapsupTaskIDs, compsupTaskIDs, err := setupTaskIDsForCreatingRecoverySystem(downloadTSS)
	if err != nil {
		return nil, err
	}

	// here we make sure that we only include the local snaps/components that
	// are actually required.
	opts.LocalComponents = usedLocalComps
	opts.LocalSnaps = usedLocalSnaps

	chg := st.NewChange(createRecoverySystemChangeKind, fmt.Sprintf("Create new recovery system with label %q", label))
	createTS, err := createRecoverySystemTasks(st, label, snapsupTaskIDs, compsupTaskIDs, opts)
	if err != nil {
		return nil, err
	}

	chg.AddAll(createTS)

	for _, ts := range downloadTSS {
		createTS.WaitAll(ts)
		chg.AddAll(ts)
	}

	return chg, nil
}

func checkForSnapIDs(model *asserts.Model, localSnaps []snapstate.PathSnap) error {
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

var errMissingLocalSnap = errors.New("missing snap from local snaps provided for offline creation of recovery system")

func offlineSnapInfo(sn *asserts.ModelSnap, rev snap.Revision, opts CreateRecoverySystemOptions) (*snap.Info, snapstate.PathSnap, error) {
	index := -1
	for i, si := range opts.LocalSnaps {
		if sn.ID() == si.SideInfo.SnapID {
			index = i
			break
		}
	}
	if index == -1 {
		return nil, snapstate.PathSnap{}, fmt.Errorf("%w: %q, rev %v", errMissingLocalSnap, sn.Name, rev)
	}

	localSnap := opts.LocalSnaps[index]

	if !rev.Unset() && rev != localSnap.SideInfo.Revision {
		return nil, snapstate.PathSnap{}, fmt.Errorf(
			"snap %q does not match revision required by validation sets: %v != %v", localSnap.SideInfo.RealName, localSnap.SideInfo.Revision, rev,
		)
	}

	s, err := snapfile.Open(localSnap.Path)
	if err != nil {
		return nil, snapstate.PathSnap{}, err
	}

	info, err := snap.ReadInfoFromSnapFile(s, localSnap.SideInfo)
	return info, localSnap, err
}

func offlineComponentInfo(cref naming.ComponentRef, rev snap.Revision, comps []snapstate.PathComponent) (snapstate.PathComponent, error) {
	index := -1
	for i, si := range comps {
		if si.SideInfo.Component == cref {
			index = i
			break
		}
	}
	if index == -1 {
		return snapstate.PathComponent{}, fmt.Errorf(
			"missing component from local components provided for offline creation of recovery system: %q, rev %v", cref, rev,
		)
	}

	comp := comps[index]

	if !rev.Unset() && rev != comp.SideInfo.Revision {
		return snapstate.PathComponent{}, fmt.Errorf(
			"component %q does not match revision required by validation sets: %v != %v", cref, comp.SideInfo.Revision, rev,
		)
	}

	return comp, nil
}

func installedSnapRevision(st *state.State, name string) (bool, snap.Revision, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, name, &snapst); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return false, snap.Revision{}, nil
		}
		return false, snap.Revision{}, err
	}
	return true, snapst.Current, nil
}

func installedComponentRevision(st *state.State, snapName, compName string) (bool, snap.Revision, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return false, snap.Revision{}, nil
		}
		return false, snap.Revision{}, err
	}

	csi := snapst.CurrentComponentSideInfo(naming.NewComponentRef(snapName, compName))
	if csi == nil {
		return false, snap.Revision{}, nil
	}
	return true, csi.Revision, nil
}

func setupTaskIDsForCreatingRecoverySystem(tss []*state.TaskSet) (snapsupTaskIDs, compsupTaskIDs []string, err error) {
	for _, ts := range tss {
		t := ts.MaybeEdge(snapstate.SnapSetupEdge)
		if t == nil {
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(t)
		if err != nil {
			return nil, nil, err
		}

		// task sets that come from non-component-exclusive operations that
		// don't introduce any local modifications don't need to be considered,
		// since they won't impact how the recovery system is created.
		//
		// TODO: should snapstate.InstallComponents put a
		// LastBeforeLocalModificationsEdge on the task set that sets up all of
		// the profiles for the components? would eliminate the second half of
		// this check.
		if ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge) == nil && !snapsup.ComponentExclusiveOperation {
			continue
		}

		if !snapsup.ComponentExclusiveOperation {
			snapsupTaskIDs = append(snapsupTaskIDs, t.ID())
		}

		var compsups []string
		if err := t.Get("component-setup-tasks", &compsups); err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, nil, err
		}

		compsupTaskIDs = append(compsupTaskIDs, compsups...)
	}

	return snapsupTaskIDs, compsupTaskIDs, nil
}

// OptionalContainers is used to define the snaps and components that are
// optional in a system's model, but can be installed when installing a system.
type OptionalContainers struct {
	// Snaps is a list of optional snap names that can be installed.
	Snaps []string `json:"snaps,omitempty"`
	// Components is a mapping of snap names to lists of optional components
	// names that can be installed.
	Components map[string][]string `json:"components,omitempty"`
}

// InstallFinish creates a change that will finish the install for the given
// label and volumes. This includes writing missing volume content, seting
// up the bootloader and installing the kernel.
func InstallFinish(st *state.State, label string, onVolumes map[string]*gadget.Volume, optionalContainers *OptionalContainers) (*state.Change, error) {
	if label == "" {
		return nil, fmt.Errorf("cannot finish install with an empty system label")
	}
	if onVolumes == nil {
		return nil, fmt.Errorf("cannot finish install without volumes data")
	}

	chg := st.NewChange(installStepFinishChangeKind, fmt.Sprintf("Finish setup of run system for %q", label))
	finishTask := st.NewTask("install-finish", fmt.Sprintf("Finish setup of run system for %q", label))
	finishTask.Set("system-label", label)
	finishTask.Set("on-volumes", onVolumes)
	if optionalContainers != nil {
		finishTask.Set("optional-install", *optionalContainers)
	}
	chg.AddTask(finishTask)

	return chg, nil
}

// InstallSetupStorageEncryption creates a change that will setup the
// storage encryption for the install of the given label and
// volumes.
func InstallSetupStorageEncryption(st *state.State, label string, onVolumes map[string]*gadget.Volume, volumesAuth *device.VolumesAuthOptions) (*state.Change, error) {
	if label == "" {
		return nil, fmt.Errorf("cannot setup storage encryption with an empty system label")
	}
	if onVolumes == nil {
		return nil, fmt.Errorf("cannot setup storage encryption without volumes data")
	}
	if volumesAuth != nil {
		if err := volumesAuth.Validate(); err != nil {
			return nil, err
		}
		// Auth data must be in memory to avoid leaking credentials.
		st.Cache(volumesAuthOptionsKey{label}, volumesAuth)
	}

	chg := st.NewChange(installStepSetupStorageEncryptionChangeKind, fmt.Sprintf("Setup storage encryption for installing system %q", label))
	setupStorageEncryptionTask := st.NewTask("install-setup-storage-encryption", fmt.Sprintf("Setup storage encryption for installing system %q", label))
	setupStorageEncryptionTask.Set("system-label", label)
	setupStorageEncryptionTask.Set("on-volumes", onVolumes)
	if volumesAuth != nil {
		setupStorageEncryptionTask.Set("volumes-auth-required", true)
	}
	chg.AddTask(setupStorageEncryptionTask)

	return chg, nil
}
