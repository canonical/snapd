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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
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

	compsups := make([]ComponentSetup, 0, len(t.components))
	compSideInfos := make([]snap.ComponentSideInfo, 0, len(t.components))
	for _, comp := range t.components {
		compsups = append(compsups, ComponentSetup{
			CompSideInfo: comp.CompSideInfo,
			CompType:     comp.CompType,
			CompPath:     comp.CompPath,
			DownloadInfo: comp.DownloadInfo,

			componentInstallFlags: componentInstallFlags{
				// if we're removing the snap, then we should remove the
				// components too
				RemoveComponentPath:   opts.Flags.RemoveSnapPath,
				MultiComponentInstall: true,
			},
		})
		compSideInfos = append(compSideInfos, *comp.CompSideInfo)
	}

	flags, err := earlyChecks(st, &t.snapst, t.info, compSideInfos, opts.Flags)
	if err != nil {
		return SnapSetup{}, nil, err
	}

	// to match the behavior of the original Update and UpdateMany, we only
	// allow updating ignoring validation sets if we are working with
	// exactly one snap
	if !opts.ExpectOneSnap {
		flags.IgnoreValidation = t.snapst.IgnoreValidation
	}

	var registries []RegistryID
	for _, plug := range t.info.Plugs {
		if plug.Interface != "registry" {
			continue
		}

		account, registry, _, err := snap.RegistryPlugAttrs(plug)
		if err != nil {
			return SnapSetup{}, nil, err
		}

		registries = append(registries, RegistryID{Account: account, Registry: registry})
	}

	providerContentAttrs := defaultProviderContentAttrs(st, t.info, opts.PrereqTracker)

	return SnapSetup{
		Channel:      t.setup.Channel,
		CohortKey:    t.setup.CohortKey,
		DownloadInfo: t.setup.DownloadInfo,
		SnapPath:     t.setup.SnapPath,
		AlwaysUpdate: t.setup.AlwaysUpdate,

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
		Registries:         registries,
	}, compsups, nil
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

		seen[sn.InstanceName] = true
		unique = append(unique, sn)
	}

	return &storeInstallGoal{
		snaps: unique,
	}
}

func validateRevisionOpts(opts *RevisionOptions) error {
	if opts.CohortKey != "" && !opts.Revision.Unset() {
		return errors.New("cannot specify revision and cohort")
	}

	// if we're leaving the cohort, clear out any provided cohort key
	if opts.LeaveCohort {
		opts.CohortKey = ""
	}

	return nil
}

var ErrExpectedOneSnap = errors.New("expected exactly one snap to install/update")

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

	if err := s.validateAndPrune(allSnaps, opts); err != nil {
		return nil, err
	}

	enforcedSetsFunc := cachedEnforcedValidationSets(st)

	includeResources := false
	actions := make([]*store.SnapAction, 0, len(s.snaps))
	for _, sn := range s.snaps {
		action := &store.SnapAction{
			Action:       "install",
			InstanceName: sn.InstanceName,
		}

		if err := completeStoreAction(action, sn.RevOpts, opts.Flags.IgnoreValidation, enforcedSetsFunc); err != nil {
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

		var channel string
		switch {
		case r.RedirectChannel != "":
			channel = r.RedirectChannel
		case sn.RevOpts.Channel != "":
			channel = sn.RevOpts.Channel
		default:
			// this should only ever happen if the caller requested a specific
			// revision to be installed (without specifying a channel). note
			// that we won't actually end up tracking "stable", it will get
			// mapped to "latest/stable" by SnapState.SetTrackingChannel in
			// doLinkSnap
			channel = "stable"
		}

		comps, err := componentTargetsFromActionResult("install", r, sn.Components)
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

// cachedEnforcedValidationSets returns a function that will lazily load (and
// cache) the enforced validation sets if is is ever called.
func cachedEnforcedValidationSets(st *state.State) func() (*snapasserts.ValidationSets, error) {
	var vsets *snapasserts.ValidationSets
	return func() (*snapasserts.ValidationSets, error) {
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
}

func componentTargetsFromActionResult(action string, sar store.SnapActionResult, requested []string) ([]ComponentSetup, error) {
	mapping := make(map[string]store.SnapResourceResult, len(sar.Resources))
	for _, res := range sar.Resources {
		mapping[res.Name] = res
	}

	setups := make([]ComponentSetup, 0, len(requested))
	for _, comp := range requested {
		res, ok := mapping[comp]
		if !ok {
			// TODO:COMPS: make sure this branch is tested when we add support for
			// losing components during a refresh
			// during a refresh, we will not install components that don't exist
			// in the new revision
			if action == "refresh" {
				continue
			}

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

	typ, err := store.ResourceToComponentType(sar.Type)
	if err != nil {
		return ComponentSetup{}, fmt.Errorf("%q is not a component resource", sar.Type)
	}
	if typ != comp.Type {
		return ComponentSetup{}, fmt.Errorf("inconsistent component type (%q in snap, %q in component)", comp.Type, typ)
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

func completeStoreAction(action *store.SnapAction, revOpts RevisionOptions, ignoreValidation bool, enforcedSets func() (*snapasserts.ValidationSets, error)) error {
	if action.Action == "" {
		return errors.New("internal error: action must be set")
	}

	if action.InstanceName == "" {
		return errors.New("internal error: instance name must be set")
	}

	action.Channel = revOpts.Channel
	action.CohortKey = revOpts.CohortKey
	action.Revision = revOpts.Revision

	switch {
	case ignoreValidation:
		// caller requested that we ignore validation sets, nothing to do
		action.Flags = store.SnapActionIgnoreValidation
	case len(revOpts.ValidationSets) > 0:
		// caller provided some validation sets, nothing to do but send them
		// to the store
		action.ValidationSets = revOpts.ValidationSets

		// the channel here should be cleared out. if the validation sets that
		// we are sending require a specific revision, we don't know if that
		// revision will be part of any requested channel. the caller still
		// might choose to track any channel in the RevisionOptions.
		action.Channel = ""
	default:
		vsets, err := enforcedSets()
		if err != nil {
			return err
		}

		// if the caller didn't provide any validation sets, make sure that
		// the snap is allowed by all of the enforced validation sets
		invalidSets, err := vsets.CheckPresenceInvalid(naming.Snap(action.InstanceName))
		if err != nil {
			if _, ok := err.(*snapasserts.PresenceConstraintError); !ok {
				return err
			} // else presence is optional or required, carry on
		}

		if len(invalidSets) > 0 {
			verb := "install"
			if action.Action == "refresh" {
				verb = "update"
			}

			return fmt.Errorf(
				"cannot %s snap %q due to enforcing rules of validation set %s",
				verb,
				action.InstanceName,
				snapasserts.ValidationSetKeySlice(invalidSets).CommaSeparated(),
			)
		}

		requiredSets, requiredRev, err := vsets.CheckPresenceRequired(naming.Snap(action.InstanceName))
		if err != nil {
			return err
		}

		// make sure that the caller-requested revision matches the revision
		// required by the enforced validation sets
		if !requiredRev.Unset() && !revOpts.Revision.Unset() && requiredRev != revOpts.Revision {
			return invalidRevisionError(action, requiredSets, revOpts.Revision, requiredRev)
		}

		// TODO:COMPS: handle validation sets and components here

		action.ValidationSets = requiredSets

		if !requiredRev.Unset() {
			// make sure that we use the revision required by the enforced
			// validation sets
			action.Revision = requiredRev

			// we ignore the cohort if a validation set requires that the
			// snap is pinned to a specific revision
			action.CohortKey = ""

			// since we're constraining this snap to a revision required by a
			// validation set, we shouldn't supply a channel.
			action.Channel = ""
		}
	}

	return nil
}

func invalidRevisionError(a *store.SnapAction, sets []snapasserts.ValidationSetKey, requested, required snap.Revision) error {
	verb := "install"
	preposition := "at"
	if a.Action == "refresh" {
		verb = "update"
		preposition = "to"
	}

	return fmt.Errorf(
		"cannot %s snap %q %s revision %s without --ignore-validation, revision %s is required by validation sets: %s",
		verb,
		a.InstanceName,
		preposition,
		requested,
		required,
		snapasserts.ValidationSetKeySlice(sets).CommaSeparated(),
	)
}

func (s *storeInstallGoal) validateAndPrune(installedSnaps map[string]*SnapState, opts Options) error {
	uninstalled := s.snaps[:0]
	for _, sn := range s.snaps {
		if err := snap.ValidateInstanceName(sn.InstanceName); err != nil {
			return fmt.Errorf("invalid instance name: %v", err)
		}

		if err := validateRevisionOpts(&sn.RevOpts); err != nil {
			return fmt.Errorf("invalid revision options for snap %q: %w", sn.InstanceName, err)
		}

		snapst, ok := installedSnaps[sn.InstanceName]
		if ok && snapst.IsInstalled() {
			if !sn.SkipIfPresent {
				return &snap.AlreadyInstalledError{Snap: sn.InstanceName}
			}
			continue
		}

		// only provide a default the channel if the revision is not set, since
		// we don't want to prevent the user from installing a specific revision
		// that doesn't happen to exist in the "stable" risk
		if sn.RevOpts.Channel == "" && sn.RevOpts.Revision.Unset() {
			sn.RevOpts.Channel = "stable"
		}

		sn.RevOpts.resolveChannel(sn.InstanceName, "stable", opts.DeviceCtx)

		uninstalled = append(uninstalled, sn)
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

// generateLane returns the lane to use for the tasks that all operate on a
// single snap. If the transaction is set to "all-snaps", then the lane is
// explicitly set to the lane provided in the options. If the transaction is set
// to "per-snap", then a new lane is generated for this snap. If the transaction
// is not set, then no lane (lane 0) is used.
//
// TODO: It might be good to consider eliminating the usage of an empty string
// for transactions, and make "per-snap" be the default in that case. There are
// some inconsistencies with how various Install/Update functions handle the
// empty string. For example, UpdateMany and InstallMany use the empty string to
// mean "per-snap", but Install uses the empty string to mean "no lane".
// Currently, UpdateWithGoal and InstallWithGoal both use the empty string to
// mean "no lane", and places that implicitly used the empty string as
// "per-snap" have been changed to use "per-snap" explicitly.
func generateLane(st *state.State, opts Options) int {
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
	if err != nil {
		return err
	}

	if opts.PrereqTracker == nil {
		opts.PrereqTracker = snap.SimplePrereqTracker{}
	}

	return nil
}

// pathInstallGoal represents a single snap to be installed from a path on disk.
type pathInstallGoal struct {
	snap PathSnap
}

// PathInstallGoal creates a new InstallGoal to install a snap from a given from
// a path on disk. If instanceName is not provided, si.RealName will be used.
func PathInstallGoal(instanceName, path string, si *snap.SideInfo, components map[*snap.ComponentSideInfo]string, opts RevisionOptions) InstallGoal {
	if instanceName == "" {
		instanceName = si.RealName
	}

	return &pathInstallGoal{
		snap: PathSnap{
			InstanceName: instanceName,
			Path:         path,
			RevOpts:      opts,
			SideInfo:     si,
			Components:   components,
		},
	}
}

// toInstall returns the data needed to setup the snap from disk.
func (p *pathInstallGoal) toInstall(ctx context.Context, st *state.State, opts Options) ([]target, error) {
	var snapst SnapState
	if err := Get(st, p.snap.InstanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	t, err := targetForPathSnap(p.snap, snapst, opts)
	if err != nil {
		return nil, err
	}
	return []target{t}, nil
}

func componentSetupsFromPaths(snapInfo *snap.Info, components map[*snap.ComponentSideInfo]string) ([]ComponentSetup, error) {
	setups := make([]ComponentSetup, 0, len(components))
	for csi, path := range components {
		compInfo, err := validatedComponentInfo(path, snapInfo, csi)
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

func validatedComponentInfo(path string, si *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
	if err := csi.Component.Validate(); err != nil {
		return nil, err
	}
	componentInfo, cont, err := backend.OpenComponentFile(path, si, csi)
	if err != nil {
		return nil, fmt.Errorf("cannot open snap file: %v", err)
	}
	if err := snap.ValidateComponentContainer(cont, csi.Component.String(), logger.Noticef); err != nil {
		return nil, err
	}
	return componentInfo, nil
}

// updatePlan contains the data that describes an update, including a list of
// target structs that represent the snaps that are to be updated.
type updatePlan struct {
	// requested is the list of snaps that were requested to be updated. If
	// RefreshAll is true, then this list should be empty.
	requested []string
	// targets is the list of snaps that are to be updated. Note that this list
	// does not necessarily match the list of snaps in requested.
	targets []target
}

// refreshAll returns true if all snaps on the system are being refreshed (could
// be either an auto-refresh or something like a manual "snap refresh").
func (p *updatePlan) refreshAll() bool {
	return len(p.requested) == 0
}

// TODO: consider making functions that call this method (and functions that
// accept the output of this method) into methods on updatePlan
func (p *updatePlan) targetInfos() []*snap.Info {
	infos := make([]*snap.Info, 0, len(p.targets))
	for _, t := range p.targets {
		infos = append(infos, t.info)
	}
	return infos
}

// updates returns the updates that should be applied to the system's state for
// this plan.
func (p *updatePlan) updates(st *state.State, opts Options) ([]update, error) {
	updates := make([]update, 0, len(p.targets))
	for _, t := range p.targets {
		opts.PrereqTracker.Add(t.info)

		snapsup, compsups, err := t.setups(st, opts)
		if err != nil {
			if !p.refreshAll() {
				return nil, err
			}

			logger.Noticef("cannot refresh snap %q: %v", t.info.InstanceName(), err)
			continue
		}

		updates = append(updates, update{
			Setup:      snapsup,
			SnapState:  t.snapst,
			Components: compsups,
		})
	}
	return updates, nil
}

// revisionChanges returns the snaps that will have their revisions changed by
// the updates in this plan.
func (p *updatePlan) revisionChanges(st *state.State, opts Options) ([]*snap.Info, error) {
	updates, err := p.updates(st, opts)
	if err != nil {
		return nil, err
	}

	targetByName := make(map[string]target, len(p.targets))
	for _, t := range p.targets {
		targetByName[t.info.InstanceName()] = t
	}

	changes := make([]*snap.Info, 0, len(updates))
	for _, up := range updates {
		if up.revisionSatisfied() {
			continue
		}

		t, ok := targetByName[up.SnapState.InstanceName()]
		// this should never happen
		if !ok {
			return nil, fmt.Errorf("internal error: update %q not found in targets", up.SnapState.InstanceName())
		}

		changes = append(changes, t.info)
	}
	return changes, nil
}

// filter applies the given function to each target in the update plan and
// removes any targets for which the function returns false.
func (p *updatePlan) filter(f func(t target) (bool, error)) error {
	filtered := p.targets[:0]
	for _, t := range p.targets {
		ok, err := f(t)
		if err != nil {
			return err
		}

		if ok {
			filtered = append(filtered, t)
		}
	}
	p.targets = filtered
	return nil
}

// filterHeldSnaps removes any targets from the update plan that are held.
// If the update plan is not refreshing all snaps, then this function does
// nothing.
func (p *updatePlan) filterHeldSnaps(st *state.State, opts Options) error {
	// we only filter out held snaps during auto-refresh or general refreshes
	// that do not specify specific snaps
	if !p.refreshAll() {
		return nil
	}

	holdLevel := HoldGeneral
	if opts.Flags.IsAutoRefresh {
		holdLevel = HoldAutoRefresh
	}

	heldSnaps, err := HeldSnaps(st, holdLevel)
	if err != nil {
		return err
	}

	p.filter(func(t target) (bool, error) {
		_, ok := heldSnaps[t.info.InstanceName()]
		return !ok, nil
	})

	return nil
}

// validateAndFilterTargets validates the targets in the update plan against the
// enforced validation sets. Any targets that cannot be validated are removed
// from the update plan.
func (p *updatePlan) validateAndFilterTargets(st *state.State, opts Options) error {
	if ValidateRefreshes == nil || len(p.targets) == 0 || opts.Flags.IgnoreValidation {
		return nil
	}

	ignoreValidation := make(map[string]bool, len(p.targets))
	for _, t := range p.targets {
		if t.snapst.IgnoreValidation {
			ignoreValidation[t.info.InstanceName()] = true
		}
	}

	validated, err := ValidateRefreshes(st, p.targetInfos(), ignoreValidation, opts.UserID, opts.DeviceCtx)
	if err != nil {
		if !p.refreshAll() {
			return err
		}
		logger.Noticef("cannot refresh some snaps: %v", err)
	}

	validatedMap := make(map[string]bool, len(validated))
	for _, sn := range validated {
		validatedMap[sn.InstanceName()] = true
	}

	p.filter(func(t target) (bool, error) {
		_, ok := validatedMap[t.info.InstanceName()]
		return ok, nil
	})

	return nil
}

// UpdateGoal represents a single snap or a group of snaps to be updated.
type UpdateGoal interface {
	// toUpdate returns the data needed to update the snaps.
	toUpdate(context.Context, *state.State, Options) (updatePlan, error)
}

// UpdateOne is a convenience wrapper for UpdateWithGoal that ensures that a
// single snap is being updated and unwraps the results to return a single
// state.TaskSet. If the UpdateGoal does not request to update exactly one snap,
// an error is returned.
func UpdateOne(ctx context.Context, st *state.State, goal UpdateGoal, filter updateFilter, opts Options) (*state.TaskSet, error) {
	opts.ExpectOneSnap = true

	updated, uts, err := UpdateWithGoal(ctx, st, goal, filter, opts)
	if err != nil {
		return nil, err
	}

	if len(updated) != 1 || len(uts.Refresh) != 1 {
		return nil, ErrExpectedOneSnap
	}

	return uts.Refresh[0], nil
}

// UpdateWithGoal updates the snap/set of snaps specified by the given
// UpdateGoal.
func UpdateWithGoal(ctx context.Context, st *state.State, goal UpdateGoal, filter updateFilter, opts Options) ([]string, *UpdateTaskSets, error) {
	if err := setDefaultSnapstateOptions(st, &opts); err != nil {
		return nil, nil, err
	}

	if opts.ExpectOneSnap && opts.Flags.IsAutoRefresh {
		return nil, nil, errors.New("internal error: auto-refresh is not supported when updating a single snap")
	}

	// can only specify a lane when running multiple operations transactionally
	if opts.Flags.Transaction != client.TransactionAllSnaps && opts.Flags.Lane != 0 {
		return nil, nil, errors.New("cannot specify a lane without setting transaction to \"all-snaps\"")
	}

	plan, err := goal.toUpdate(ctx, st, opts)
	if err != nil {
		return nil, nil, err
	}

	if opts.ExpectOneSnap && len(plan.targets) != 1 {
		return nil, nil, ErrExpectedOneSnap
	}

	if filter != nil {
		plan.filter(func(t target) (bool, error) {
			return filter(t.info, &t.snapst), nil
		})
	}

	if err := plan.filterHeldSnaps(st, opts); err != nil {
		return nil, nil, err
	}

	// save the candidates so the auto-refresh can be continued if it's inhibited
	// by a running snap.
	if opts.Flags.IsAutoRefresh {
		hints, err := refreshHintsFromUpdatePlan(st, plan, opts.DeviceCtx)
		if err != nil {
			return nil, nil, err
		}

		// TODO: why not check this error?
		updateRefreshCandidates(st, hints, plan.requested)
	}

	// validate snaps to be refreshed against validation sets. if we are
	// refreshing all snaps, then we filter out the snaps that cannot be
	// validated and log them
	if err := plan.validateAndFilterTargets(st, opts); err != nil {
		return nil, nil, err
	}

	changeKind := "refresh"
	installInfos := make([]minimalInstallInfo, 0, len(plan.targets))
	for _, t := range plan.targets {
		installInfos = append(installInfos, installSnapInfo{t.info})

		// if any of the snaps are not installed, then we should use the
		// "install" change as the kind
		if !t.snapst.IsInstalled() {
			changeKind = "install"
		}
	}

	if err := checkDiskSpace(st, changeKind, installInfos, opts.UserID, opts.PrereqTracker); err != nil {
		return nil, nil, err
	}

	updated, uts, err := updateFromPlan(st, plan, opts)
	if err != nil {
		return nil, nil, err
	}

	// ideally we wouldn't use this error type here, but the current
	// implementations share this error type for both path and store
	// installations
	if opts.ExpectOneSnap && len(uts.Refresh) == 0 {
		return nil, nil, store.ErrNoUpdateAvailable
	}

	return updated, uts, nil
}

func updateFromPlan(st *state.State, plan updatePlan, opts Options) ([]string, *UpdateTaskSets, error) {
	// it is sad that we have to split up updatePlan like this, but doUpdate is
	// used in places where we don't have a snap.Info, so we cannot pass an
	// updatePlan to doUpdate
	updates, err := plan.updates(st, opts)
	if err != nil {
		return nil, nil, err
	}

	updated, uts, err := doPotentiallySplitUpdate(st, plan.requested, updates, opts)
	if err != nil {
		return nil, nil, err
	}

	// if we're only updating one snap, flatten everything into one task set
	if opts.ExpectOneSnap && len(uts.Refresh) > 1 {
		flat := state.NewTaskSet()
		for _, ts := range uts.Refresh {
			// The tasksets we get from "doUpdate" contain important "TaskEdge"
			// information that is needed for "Remodel". To preserve those we
			// need to use "AddAllWithEdges()".
			if err := flat.AddAllWithEdges(ts); err != nil {
				return nil, nil, err
			}
		}
		uts.Refresh = []*state.TaskSet{flat}
	}

	return updated, uts, nil
}

// storeInstallGoal implements the UpdateGoal interface and represents a group
// of snaps that are to be updated from the store.
type storeUpdateGoal struct {
	// snaps is a mapping of snap names to StoreUpdate structs.
	snaps map[string]StoreUpdate
}

// StoreUpdate represents a snap that is to be updated from the store.
type StoreUpdate struct {
	// InstanceName is the instance name of the snap to update.
	InstanceName string
	// RevOpts contains options that apply to the update of this snap.
	RevOpts RevisionOptions
}

// StoreUpdateGoal creates a new UpdateGoal to update snaps from the store.
func StoreUpdateGoal(snaps ...StoreUpdate) UpdateGoal {
	mapping := make(map[string]StoreUpdate, len(snaps))
	for _, sn := range snaps {
		if _, ok := mapping[sn.InstanceName]; ok {
			continue
		}

		mapping[sn.InstanceName] = sn
	}

	return &storeUpdateGoal{
		snaps: mapping,
	}
}

func (s *storeUpdateGoal) toUpdate(ctx context.Context, st *state.State, opts Options) (updatePlan, error) {
	if opts.ExpectOneSnap && len(s.snaps) != 1 {
		return updatePlan{}, ErrExpectedOneSnap
	}

	allSnaps, err := All(st)
	if err != nil {
		return updatePlan{}, err
	}

	if err := validateAndInitStoreUpdates(allSnaps, s.snaps, opts); err != nil {
		return updatePlan{}, err
	}

	user, err := userFromUserID(st, opts.UserID)
	if err != nil {
		return updatePlan{}, err
	}

	refreshOpts := &store.RefreshOptions{Scheduled: opts.Flags.IsAutoRefresh}
	plan, err := storeUpdatePlan(ctx, st, allSnaps, s.snaps, user, refreshOpts, opts)
	if err != nil {
		return updatePlan{}, err
	}

	return plan, nil
}

func validateAndInitStoreUpdates(allSnaps map[string]*SnapState, updates map[string]StoreUpdate, opts Options) error {
	for _, sn := range updates {
		snapst, ok := allSnaps[sn.InstanceName]
		if !ok {
			return snap.NotInstalledError{Snap: sn.InstanceName}
		}

		// default to existing cohort key if we don't have a provided one
		if sn.RevOpts.CohortKey == "" && !sn.RevOpts.LeaveCohort {
			sn.RevOpts.CohortKey = snapst.CohortKey
		}

		if err := sn.RevOpts.resolveChannel(sn.InstanceName, snapst.TrackingChannel, opts.DeviceCtx); err != nil {
			return err
		}

		updates[sn.InstanceName] = sn
	}

	return nil
}

// PathSnap represents a single snap to be installed or updated from a path on
// disk.
type PathSnap struct {
	// Path is the path to the snap on disk.
	Path string
	// InstanceName is the name of the snap.
	InstanceName string
	// RevOpts contains options that apply to the installation or update of this
	// snap.
	RevOpts RevisionOptions
	// SideInfo contains extra information about the snap.
	SideInfo *snap.SideInfo
	// Components is a mapping of component side infos to paths that should be
	// installed alongside this snap.
	Components map[*snap.ComponentSideInfo]string
}

// pathUpdateGoal implements the UpdateGoal interface and represents a group of
// snaps that are to be updated from paths on disk.
type pathUpdateGoal struct {
	updates []PathSnap
}

// PathUpdateGoal creates a new UpdateGoal to update snaps from paths on disk.
func PathUpdateGoal(snaps ...PathSnap) UpdateGoal {
	seen := make(map[string]bool)
	filtered := make([]PathSnap, 0, len(snaps))
	for _, snap := range snaps {
		if snap.InstanceName == "" {
			snap.InstanceName = snap.SideInfo.RealName
		}

		if seen[snap.InstanceName] {
			continue
		}

		seen[snap.InstanceName] = true
		filtered = append(filtered, snap)
	}

	return &pathUpdateGoal{
		updates: filtered,
	}
}

func (p *pathUpdateGoal) toUpdate(_ context.Context, st *state.State, opts Options) (updatePlan, error) {
	targets := make([]target, 0, len(p.updates))
	names := make([]string, 0, len(p.updates))

	for _, sn := range p.updates {
		var snapst SnapState
		if err := Get(st, sn.InstanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
			return updatePlan{}, err
		}

		t, err := targetForPathSnap(sn, snapst, opts)
		if err != nil {
			return updatePlan{}, err
		}

		// TODO:COMPS: remove this once we are ready to handle components during
		// refresh
		t.components = nil

		targets = append(targets, t)
		names = append(names, sn.InstanceName)
	}

	return updatePlan{
		targets:   targets,
		requested: names,
	}, nil
}

func targetForPathSnap(update PathSnap, snapst SnapState, opts Options) (target, error) {
	si := update.SideInfo

	if si.RealName == "" {
		return target{}, fmt.Errorf("internal error: snap name to install %q not provided", update.Path)
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return target{}, fmt.Errorf("internal error: snap id set to install %q but revision is unset", update.Path)
		}
	}

	if err := snap.ValidateInstanceName(update.InstanceName); err != nil {
		return target{}, fmt.Errorf("invalid instance name: %v", err)
	}

	if err := validateRevisionOpts(&update.RevOpts); err != nil {
		return target{}, fmt.Errorf("invalid revision options for snap %q: %w", update.InstanceName, err)
	}

	if !update.RevOpts.Revision.Unset() && update.RevOpts.Revision != si.Revision {
		return target{}, fmt.Errorf("cannot install local snap %q: %v != %v (revision mismatch)", update.InstanceName, update.RevOpts.Revision, si.Revision)
	}

	if update.RevOpts.Channel != "" && update.SideInfo.Channel != "" && update.RevOpts.Channel != update.SideInfo.Channel {
		return target{}, fmt.Errorf("cannot install local snap %q: %v != %v (channel mismatch)", update.InstanceName, update.RevOpts.Channel, si.Channel)
	}

	info, err := validatedInfoFromPathAndSideInfo(update.InstanceName, update.Path, si)
	if err != nil {
		return target{}, err
	}

	var trackingChannel string
	if snapst.IsInstalled() {
		trackingChannel = snapst.TrackingChannel
	}

	if update.RevOpts.Channel == "" {
		update.RevOpts.Channel = update.SideInfo.Channel
	}

	channel, err := resolveChannel(update.InstanceName, trackingChannel, update.RevOpts.Channel, opts.DeviceCtx)
	if err != nil {
		return target{}, err
	}

	comps, err := componentSetupsFromPaths(info, update.Components)
	if err != nil {
		return target{}, err
	}

	return target{
		setup: SnapSetup{
			SnapPath:  update.Path,
			Channel:   channel,
			CohortKey: update.RevOpts.CohortKey,
		},
		info:       info,
		snapst:     snapst,
		components: comps,
	}, nil
}
