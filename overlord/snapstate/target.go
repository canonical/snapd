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

package snapstate

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// Options contains optional parameters for the snapstate operations. All of
// these fields are optional and can be left unset. The options in this struct
// apply to all snaps that are part of an operation. Options that apply to
// individual snaps can be found in RevisionOptions.
type Options struct {
	// Flags contains flags that apply to the entire operation.
	Flags Flags
	// UserID is the ID of the user that is performing the operation.
	UserID int
	// DeviceCtx is an optional device context that will be used during the
	// operation.
	DeviceCtx DeviceContext
	// PrereqTracker is an optional prereq tracker that will be used to keep
	// track of all snaps (explicitly requested and implicitly required snaps)
	// that might need to be installed during the operation.
	PrereqTracker PrereqTracker
	// FromChange is the change that triggered the operation.
	FromChange string
	// Seed should be true while seeding the device. This indicates that we
	// shouldn't require that the device is seeded before installing/updating
	// snaps.
	Seed bool
	// ExpectOneSnap is a boolean flag indicating that this operation is expected
	// to only operate on one snap (excluding any prerequisite snaps that may be
	// required). If this is true, then the operation will fail if more than one
	// snap is being operated on. This flag primarily exists to support the
	// pre-existing behavior of calling InstallMany with one snap vs calling
	// Install.
	ExpectOneSnap bool
}

// target represents the data needed to setup a snap for installation.
type target struct {
	// setup is a partially initialized SnapSetup that contains the data needed
	// to find the snap file that will be installed.
	setup SnapSetup
	// info contains the snap.info for the snap to be installed.
	info *snap.Info
	// snapst is the current state of the target snap, prior to installation.
	// This must be retrieved prior to unlocking the state for any reason (for
	// example, talking to the store).
	snapst SnapState
	// components is a list of components to install with this snap.
	components []ComponentSetup
}

// setups returns the completed SnapSetup and slice of ComponentSetup structs
// for the target snap.
func (t *target) setups(st *state.State, opts Options) (SnapSetup, []ComponentSetup, error) {
	snapUserID, err := userIDForSnap(st, &t.snapst, opts.UserID)
	if err != nil {
		return SnapSetup{}, nil, err
	}

	flags, err := earlyChecks(st, &t.snapst, t.info, opts.Flags)
	if err != nil {
		return SnapSetup{}, nil, err
	}

	providerContentAttrs := defaultProviderContentAttrs(st, t.info, opts.PrereqTracker)
	return SnapSetup{
		Channel:      t.setup.Channel,
		CohortKey:    t.setup.CohortKey,
		DownloadInfo: t.setup.DownloadInfo,
		SnapPath:     t.setup.SnapPath,

		Base:               t.info.Base,
		Prereq:             getKeys(providerContentAttrs),
		PrereqContentAttrs: providerContentAttrs,
		UserID:             snapUserID,
		Flags:              flags.ForSnapSetup(),
		SideInfo:           &t.info.SideInfo,
		Type:               t.info.Type(),
		Version:            t.info.Version,
		PlugsOnly:          len(t.info.Slots) == 0,
		InstanceKey:        t.info.InstanceKey,
		ExpectedProvenance: t.info.SnapProvenance,
	}, t.components, nil
}

// InstallGoal represents a single snap or a group of snaps to be installed.
type InstallGoal interface {
	// toInstall returns the data needed to setup the snaps for installation.
	toInstall(context.Context, *state.State, Options) ([]target, error)
}

// storeInstallGoal implements the InstallGoal interface and represents a group of
// snaps that are to be installed from the store.
type storeInstallGoal struct {
	// snaps is a slice of StoreSnap structs that contains details about the
	// snap to install. It maintains the order of the snaps as they were
	// provided.
	snaps []StoreSnap
}

func (s *storeInstallGoal) find(name string) (StoreSnap, bool) {
	for _, sn := range s.snaps {
		if sn.InstanceName == name {
			return sn, true
		}
	}
	return StoreSnap{}, false
}

// StoreSnap represents a snap that is to be installed from the store.
type StoreSnap struct {
	// InstanceName is the name of snap to install.
	InstanceName string
	// Components is the list of components to install with this snap.
	Components []string
	// RevOpts contains options that apply to the installation of this snap.
	RevOpts RevisionOptions
	// SkipIfPresent indicates that the snap should not be installed if it is already present.
	SkipIfPresent bool
}

// StoreInstallGoal creates a new InstallGoal to install snaps from the store.
// If a snap is provided more than once in the list, the first instance of it
// will be used to provide the installation options.
func StoreInstallGoal(snaps ...StoreSnap) InstallGoal {
	seen := make(map[string]bool, len(snaps))
	unique := make([]StoreSnap, 0, len(snaps))
	for _, sn := range snaps {
		if _, ok := seen[sn.InstanceName]; ok {
			continue
		}

		if sn.RevOpts.Channel == "" {
			sn.RevOpts.Channel = "stable"
		}

		if len(sn.Components) > 0 {
			sn.Components = strutil.Deduplicate(sn.Components)
		}

		seen[sn.InstanceName] = true
		unique = append(unique, sn)
	}

	return &storeInstallGoal{
		snaps: unique,
	}
}

func validateRevisionOpts(opts RevisionOptions) error {
	if opts.CohortKey != "" && !opts.Revision.Unset() {
		return errors.New("cannot specify revision and cohort")
	}

	return nil
}

var ErrExpectedOneSnap = errors.New("expected exactly one snap to install")

// toInstall returns the data needed to setup the snaps from the store for
// installation.
func (s *storeInstallGoal) toInstall(ctx context.Context, st *state.State, opts Options) ([]target, error) {
	if opts.ExpectOneSnap && len(s.snaps) != 1 {
		return nil, ErrExpectedOneSnap
	}

	allSnaps, err := All(st)
	if err != nil {
		return nil, err
	}

	if err := s.validateAndPrune(allSnaps); err != nil {
		return nil, err
	}

	// create a closure that will lazily load the enforced validation sets if
	// any of the targets require them
	var vsets *snapasserts.ValidationSets
	enforcedSets := func() (*snapasserts.ValidationSets, error) {
		if vsets != nil {
			return vsets, nil
		}

		var err error
		vsets, err = EnforcedValidationSets(st)
		if err != nil {
			return nil, err
		}

		return vsets, nil
	}

	includeResources := false
	actions := make([]*store.SnapAction, 0, len(s.snaps))
	for _, sn := range s.snaps {
		action, err := installActionForStoreTarget(sn, opts, enforcedSets)
		if err != nil {
			return nil, err
		}

		if len(sn.Components) > 0 {
			includeResources = true
		}

		actions = append(actions, action)
	}

	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	refreshOpts, err := refreshOptions(st, &store.RefreshOptions{
		IncludeResources: includeResources,
	})
	if err != nil {
		return nil, err
	}

	user, err := userFromUserID(st, opts.UserID)
	if err != nil {
		return nil, err
	}

	str := Store(st, opts.DeviceCtx)

	st.Unlock() // calls to the store should be done without holding the state lock
	results, _, err := str.SnapAction(context.TODO(), curSnaps, actions, nil, user, refreshOpts)
	st.Lock()

	if err != nil {
		if opts.ExpectOneSnap {
			return nil, singleActionResultErr(actions[0].InstanceName, actions[0].Action, err)
		}
		return nil, err
	}

	installs := make([]target, 0, len(results))
	for _, r := range results {
		sn, ok := s.find(r.InstanceName())
		if !ok {
			return nil, fmt.Errorf("store returned unsolicited snap action: %s", r.InstanceName())
		}

		snapst, ok := allSnaps[r.InstanceName()]
		if !ok {
			snapst = &SnapState{}
		}

		// TODO: is it safe to pull the channel from here? i'm not sure what
		// this will actually look like as a response from the real store
		channel := r.RedirectChannel
		if r.RedirectChannel == "" {
			channel = sn.RevOpts.Channel
		}

		comps, err := requestedComponentsFromActionResult(sn, r)
		if err != nil {
			return nil, fmt.Errorf("cannot extract components from snap resources: %w", err)
		}

		installs = append(installs, target{
			setup: SnapSetup{
				DownloadInfo: &r.DownloadInfo,
				Channel:      channel,
				CohortKey:    sn.RevOpts.CohortKey,
			},
			info:       r.Info,
			snapst:     *snapst,
			components: comps,
		})
	}

	return installs, err
}

func requestedComponentsFromActionResult(sn StoreSnap, sar store.SnapActionResult) ([]ComponentSetup, error) {
	mapping := make(map[string]store.SnapResourceResult, len(sar.Resources))
	for _, res := range sar.Resources {
		mapping[res.Name] = res
	}

	setups := make([]ComponentSetup, 0, len(sn.Components))
	for _, comp := range sn.Components {
		res, ok := mapping[comp]
		if !ok {
			return nil, fmt.Errorf("cannot find component %q in snap resources", comp)
		}

		setup, err := componentSetupFromResource(comp, res, sar.Info)
		if err != nil {
			return nil, err
		}

		setups = append(setups, setup)
	}
	return setups, nil
}

func componentSetupFromResource(name string, sar store.SnapResourceResult, info *snap.Info) (ComponentSetup, error) {
	comp, ok := info.Components[name]
	if !ok {
		return ComponentSetup{}, fmt.Errorf("%q is not a component for snap %q", name, info.SnapName())
	}

	if typ := fmt.Sprintf("component/%s", comp.Type); typ != sar.Type {
		return ComponentSetup{}, fmt.Errorf("inconsistent component type (%q in snap, %q in component)", typ, sar.Type)
	}

	cref := naming.NewComponentRef(info.SnapName(), name)

	csi := snap.ComponentSideInfo{
		Component: cref,
		Revision:  snap.R(sar.Revision),
	}

	return ComponentSetup{
		DownloadInfo: &sar.DownloadInfo,
		CompSideInfo: &csi,
		CompType:     comp.Type,
	}, nil
}

func installActionForStoreTarget(t StoreSnap, opts Options, enforcedSets func() (*snapasserts.ValidationSets, error)) (*store.SnapAction, error) {
	action := &store.SnapAction{
		Action:       "install",
		InstanceName: t.InstanceName,
		Channel:      t.RevOpts.Channel,
		Revision:     t.RevOpts.Revision,
		CohortKey:    t.RevOpts.CohortKey,
	}

	switch {
	case opts.Flags.IgnoreValidation:
		// caller requested that we ignore validation sets, nothing to do
		action.Flags = store.SnapActionIgnoreValidation
	case len(t.RevOpts.ValidationSets) > 0:
		// caller provided some validation sets, nothing to do but send them
		// to the store
		action.ValidationSets = t.RevOpts.ValidationSets
	default:
		vsets, err := enforcedSets()
		if err != nil {
			return nil, err
		}

		// if the caller didn't provide any validation sets, make sure that
		// the snap is allowed by all of the enforced validation sets
		invalidSets, err := vsets.CheckPresenceInvalid(naming.Snap(t.InstanceName))
		if err != nil {
			if _, ok := err.(*snapasserts.PresenceConstraintError); !ok {
				return nil, err
			} // else presence is optional or required, carry on
		}

		if len(invalidSets) > 0 {
			return nil, fmt.Errorf(
				"cannot install snap %q due to enforcing rules of validation set %s",
				t.InstanceName, snapasserts.ValidationSetKeySlice(invalidSets).CommaSeparated(),
			)
		}

		requiredSets, requiredRev, err := vsets.CheckPresenceRequired(naming.Snap(t.InstanceName))
		if err != nil {
			return nil, err
		}

		// make sure that the caller-requested revision matches the revision
		// required by the enforced validation sets
		if !requiredRev.Unset() && !t.RevOpts.Revision.Unset() && requiredRev != t.RevOpts.Revision {
			return nil, fmt.Errorf(
				"cannot install snap %q at requested revision %s without --ignore-validation, revision %s required by validation sets: %s",
				t.InstanceName, t.RevOpts.Revision, requiredRev, snapasserts.ValidationSetKeySlice(requiredSets).CommaSeparated(),
			)
		}

		// TODO: handle validation sets and components here

		action.ValidationSets = requiredSets

		if !requiredRev.Unset() {
			// make sure that we use the revision required by the enforced
			// validation sets
			action.Revision = requiredRev

			// we ignore the cohort if a validation set requires that the
			// snap is pinned to a specific revision
			action.CohortKey = ""
		}
	}

	// clear out the channel if we're requesting a specific revision, which
	// could be because the user requested a specific revision or because a
	// validation set requires it
	if !action.Revision.Unset() {
		action.Channel = ""
	}

	return action, nil
}

func (s *storeInstallGoal) validateAndPrune(installedSnaps map[string]*SnapState) error {
	uninstalled := s.snaps[:0]
	for _, t := range s.snaps {
		if err := snap.ValidateInstanceName(t.InstanceName); err != nil {
			return fmt.Errorf("invalid instance name: %v", err)
		}

		if err := validateRevisionOpts(t.RevOpts); err != nil {
			return fmt.Errorf("invalid revision options for snap %q: %w", t.InstanceName, err)
		}

		snapst, ok := installedSnaps[t.InstanceName]
		if ok && snapst.IsInstalled() {
			if !t.SkipIfPresent {
				return &snap.AlreadyInstalledError{Snap: t.InstanceName}
			}
			continue
		}

		uninstalled = append(uninstalled, t)
	}

	s.snaps = uninstalled

	return nil
}

// InstallOne is a convenience wrapper for InstallWithGoal that ensures that a
// single snap is being installed and unwraps the results to return a single
// snap.Info and state.TaskSet. If the InstallGoal does not request to install
// exactly one snap, an error is returned.
func InstallOne(ctx context.Context, st *state.State, goal InstallGoal, opts Options) (*snap.Info, *state.TaskSet, error) {
	opts.ExpectOneSnap = true

	infos, tasksets, err := InstallWithGoal(ctx, st, goal, opts)
	if err != nil {
		return nil, nil, err
	}

	// this case is unexpected since InstallWithGoal verifies that we are
	// operating on exactly one target
	if len(infos) != 1 || len(tasksets) != 1 {
		return nil, nil, errors.New("internal error: expected exactly one snap and task set")
	}

	return infos[0], tasksets[0], nil
}

// InstallWithGoal installs the snap/set of snaps specified by the given
// InstallGoal.
//
// The InstallGoal controls what snaps should be installed and where to source the
// snaps from. The Options struct contains optional parameters that apply to the
// installation operation.
//
// A slice of snap.Info structs is returned for each snap that is being
// installed along with a slice of state.TaskSet structs that represent the
// tasks that are part of the installation operation for each snap.
//
// TODO: rename this to Install once the API is settled, and we can rename or
// remove the old Install function.
func InstallWithGoal(ctx context.Context, st *state.State, goal InstallGoal, opts Options) ([]*snap.Info, []*state.TaskSet, error) {
	if opts.PrereqTracker == nil {
		opts.PrereqTracker = snap.SimplePrereqTracker{}
	}

	// can only specify a lane when running multiple operations transactionally
	if opts.Flags.Transaction != client.TransactionAllSnaps && opts.Flags.Lane != 0 {
		return nil, nil, errors.New("cannot specify a lane without setting transaction to \"all-snaps\"")
	}

	if opts.Flags.Transaction == client.TransactionAllSnaps && opts.Flags.Lane == 0 {
		opts.Flags.Lane = st.NewLane()
	}

	if err := setDefaultSnapstateOptions(st, &opts); err != nil {
		return nil, nil, err
	}

	targets, err := goal.toInstall(ctx, st, opts)
	if err != nil {
		return nil, nil, err
	}

	// this might be checked earlier in the implementation of InstallGoal, but
	// we should check it here as well to be safe
	if opts.ExpectOneSnap && len(targets) != 1 {
		return nil, nil, ErrExpectedOneSnap
	}

	for _, t := range targets {
		// sort the components by name to ensure we always install components in the
		// same order
		sort.Slice(t.components, func(i, j int) bool {
			return t.components[i].ComponentName() < t.components[j].ComponentName()
		})
	}

	installInfos := make([]minimalInstallInfo, 0, len(targets))
	for _, t := range targets {
		installInfos = append(installInfos, installSnapInfo{t.info})
	}

	if err = checkDiskSpace(st, "install", installInfos, opts.UserID, opts.PrereqTracker); err != nil {
		return nil, nil, err
	}

	tasksets := make([]*state.TaskSet, 0, len(targets))
	infos := make([]*snap.Info, 0, len(targets))
	for _, t := range targets {
		if t.setup.SnapPath != "" && t.setup.DownloadInfo != nil {
			return nil, nil, errors.New("internal error: target cannot specify both a path and a download info")
		}

		if opts.Flags.RequireTypeBase && t.info.Type() != snap.TypeBase && t.info.Type() != snap.TypeOS {
			return nil, nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", t.info.Type())
		}

		opts.PrereqTracker.Add(t.info)

		snapsup, compsups, err := t.setups(st, opts)
		if err != nil {
			return nil, nil, err
		}

		var instFlags int
		if opts.Flags.SkipConfigure {
			instFlags |= skipConfigure
		}

		ts, err := doInstall(st, &t.snapst, snapsup, compsups, instFlags, opts.FromChange, inUseFor(opts.DeviceCtx))
		if err != nil {
			return nil, nil, err
		}

		ts.JoinLane(generateLane(st, opts))

		tasksets = append(tasksets, ts)
		infos = append(infos, t.info)
	}

	return infos, tasksets, nil
}

func generateLane(st *state.State, opts Options) int {
	// If transactional, use a single lane for all snaps, so when
	// one fails the changes for all affected snaps will be
	// undone. Otherwise, have different lanes per snap so failures
	// only affect the culprit snap.
	switch opts.Flags.Transaction {
	case client.TransactionAllSnaps:
		return opts.Flags.Lane
	case client.TransactionPerSnap:
		return st.NewLane()
	}
	return 0
}

func setDefaultSnapstateOptions(st *state.State, opts *Options) error {
	var err error
	if opts.Seed {
		opts.DeviceCtx, err = DeviceCtxFromState(st, opts.DeviceCtx)
	} else {
		opts.DeviceCtx, err = DevicePastSeeding(st, opts.DeviceCtx)
	}
	return err
}

// pathInstallGoal represents a single snap to be installed from a path on disk.
type pathInstallGoal struct {
	// path is the path to the snap on disk.
	path string
	// instanceName is the name of the snap to install.
	instanceName string
	// revOpts contains options that apply to the installation of this snap.
	revOpts RevisionOptions
	// sideInfo contains extra information about the snap.
	sideInfo *snap.SideInfo
	// components is a mapping of component side infos to paths that should be
	// installed alongside this snap.
	components map[*snap.ComponentSideInfo]string
}

// PathInstallGoal creates a new InstallGoal to install a snap from a given from
// a path on disk. If instanceName is not provided, si.RealName will be used.
func PathInstallGoal(instanceName, path string, si *snap.SideInfo, components map[*snap.ComponentSideInfo]string, opts RevisionOptions) InstallGoal {
	return &pathInstallGoal{
		instanceName: instanceName,
		path:         path,
		revOpts:      opts,
		sideInfo:     si,
		components:   components,
	}
}

// toInstall returns the data needed to setup the snap from disk.
func (p *pathInstallGoal) toInstall(ctx context.Context, st *state.State, opts Options) ([]target, error) {
	si := p.sideInfo

	if si.RealName == "" {
		return nil, fmt.Errorf("internal error: snap name to install %q not provided", p.path)
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return nil, fmt.Errorf("internal error: snap id set to install %q but revision is unset", p.path)
		}
	}

	if p.instanceName == "" {
		p.instanceName = si.RealName
	}

	if err := snap.ValidateInstanceName(p.instanceName); err != nil {
		return nil, fmt.Errorf("invalid instance name: %v", err)
	}

	if err := validateRevisionOpts(p.revOpts); err != nil {
		return nil, fmt.Errorf("invalid revision options for snap %q: %w", p.instanceName, err)
	}

	if !p.revOpts.Revision.Unset() && p.revOpts.Revision != si.Revision {
		return nil, fmt.Errorf("cannot install local snap %q: %v != %v (revision mismatch)", p.instanceName, p.revOpts.Revision, si.Revision)
	}

	info, err := validatedInfoFromPathAndSideInfo(p.instanceName, p.path, si)
	if err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(p.instanceName)
	if info.SnapName() != snapName {
		return nil, fmt.Errorf("cannot install snap %q, the name does not match the metadata %q", p.instanceName, info.SnapName())
	}
	info.InstanceKey = instanceKey

	var snapst SnapState
	if err := Get(st, p.instanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	var trackingChannel string
	if snapst.IsInstalled() {
		trackingChannel = snapst.TrackingChannel
	}

	channel, err := resolveChannel(p.instanceName, trackingChannel, p.revOpts.Channel, opts.DeviceCtx)
	if err != nil {
		return nil, err
	}

	comps, err := installableComponentsFromPaths(info, p.components)
	if err != nil {
		return nil, err
	}

	inst := target{
		setup: SnapSetup{
			SnapPath:  p.path,
			Channel:   channel,
			CohortKey: p.revOpts.CohortKey,
		},
		info:       info,
		snapst:     snapst,
		components: comps,
	}

	return []target{inst}, nil
}

func installableComponentsFromPaths(info *snap.Info, components map[*snap.ComponentSideInfo]string) ([]ComponentSetup, error) {
	setups := make([]ComponentSetup, 0, len(components))
	for csi, path := range components {
		compInfo, _, err := backend.OpenComponentFile(path, info, csi)
		if err != nil {
			return nil, err
		}

		setups = append(setups, ComponentSetup{
			CompPath:     path,
			CompSideInfo: csi,
			CompType:     compInfo.Type,
		})
	}

	return setups, nil
}
