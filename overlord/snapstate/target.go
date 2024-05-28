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

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
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
}

// Installable represents the data needed to setup a snap for installation.
type Installable struct {
	// Snap is a partially initialized SnapSetup that contains the data needed
	// to find the snap file that will be installed.
	Snap *SnapSetup
	// Components is a list of partially initialized ComponentSetup structs that
	// contain the data needed to find the component files that will be
	// installed.
	Components []*ComponentSetup
	// Info contains the snap.Info for the snap to be installed.
	Info *snap.Info
}

// Target represents a single snap or a group of snaps to be installed.
type Target interface {
	// Installables returns the data needed to setup the snaps for installation.
	Installables(context.Context, *state.State, map[string]*SnapState, Options) ([]Installable, error)
}

// OptionInitializer is an interface that can be implemented by a Target to
// initialize the SnapstateOptions before the installation of the snaps.
type OptionInitializer interface {
	// InitOptions initializes the SnapstateOptions before the installation of
	// the snaps.
	InitOptions(*state.State, *Options) error
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

// StoreTarget implements the Target interface and represents a group of
// snaps that are to be installed from the store.
type StoreTarget struct {
	snaps map[string]StoreSnap
}

// verify that StoreTarget implements the Target interface
var _ Target = &StoreTarget{}

// NewStoreTarget creates a new StoreTarget from the given StoreSnaps.
func NewStoreTarget(snaps ...StoreSnap) *StoreTarget {
	mapping := make(map[string]StoreSnap, len(snaps))
	for _, sn := range snaps {
		if _, ok := mapping[sn.InstanceName]; ok {
			continue
		}

		if sn.RevOpts.Channel == "" {
			sn.RevOpts.Channel = "stable"
		}

		mapping[sn.InstanceName] = sn
	}

	return &StoreTarget{
		snaps: mapping,
	}
}

func validateRevisionOpts(opts RevisionOptions) error {
	if opts.CohortKey != "" && opts.Revision.Set() {
		return errors.New("cannot specify revision and cohort")
	}

	return nil
}

// Installables returns the data needed to setup the snaps from the store for
// installation.
func (s *StoreTarget) Installables(ctx context.Context, st *state.State, installedSnaps map[string]*SnapState, opts Options) ([]Installable, error) {
	if err := s.validateAndPrune(installedSnaps); err != nil {
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

	actions := make([]*store.SnapAction, 0, len(s.snaps))
	for _, sn := range s.snaps {
		action, err := installActionForStoreTarget(sn, opts, enforcedSets)
		if err != nil {
			return nil, err
		}

		actions = append(actions, action)
	}

	curSnaps, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	refreshOpts, err := refreshOptions(st, nil)
	if err != nil {
		return nil, err
	}

	user, err := userFromUserID(st, opts.UserID)
	if err != nil {
		return nil, err
	}

	str := Store(st, opts.DeviceCtx)

	st.Unlock() // calls to the store should be done without holding the state lock
	defer st.Lock()

	results, _, err := str.SnapAction(context.TODO(), curSnaps, actions, nil, user, refreshOpts)
	if err != nil {
		if len(actions) == 1 {
			return nil, singleActionResultErr(actions[0].InstanceName, actions[0].Action, err)
		}
		return nil, err
	}

	installs := make([]Installable, 0, len(results))
	for _, r := range results {
		sn, ok := s.snaps[r.InstanceName()]
		if !ok {
			return nil, fmt.Errorf("store returned unsolicited snap action: %s", r.InstanceName())
		}

		// TODO: extract components from resources

		// TODO: is it safe to pull the channel from here? i'm not sure what
		// this will actually look like as a response from the real store
		channel := r.RedirectChannel
		if r.RedirectChannel == "" {
			channel = sn.RevOpts.Channel
		}

		installs = append(installs, Installable{
			Snap: &SnapSetup{
				DownloadInfo: &r.DownloadInfo,
				Channel:      channel,
				CohortKey:    sn.RevOpts.CohortKey,
			},
			Info: r.Info,
		})
	}

	return installs, err
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
		if requiredRev.Set() && t.RevOpts.Revision.Set() && requiredRev != t.RevOpts.Revision {
			return nil, fmt.Errorf(
				"cannot install snap %q at requested revision %s without --ignore-validation, revision %s required by validation sets: %s",
				t.InstanceName, t.RevOpts.Revision, requiredRev, snapasserts.ValidationSetKeySlice(requiredSets).CommaSeparated(),
			)
		}

		// TODO: handle validation sets and components here

		action.ValidationSets = requiredSets

		if requiredRev.Set() {
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
	if action.Revision.Set() {
		action.Channel = ""
	}

	return action, nil
}

func (s *StoreTarget) validateAndPrune(installedSnaps map[string]*SnapState) error {
	for name, t := range s.snaps {
		if err := snap.ValidateInstanceName(name); err != nil {
			return fmt.Errorf("invalid instance name: %v", err)
		}

		if err := validateRevisionOpts(t.RevOpts); err != nil {
			return fmt.Errorf("invalid revision options for snap %q: %w", name, err)
		}

		snapst, ok := installedSnaps[name]
		if ok && snapst.IsInstalled() {
			if !t.SkipIfPresent {
				return &snap.AlreadyInstalledError{Snap: name}
			}
			delete(s.snaps, name)
		}
	}
	return nil
}

// InstallOne is a wrapper for InstallTarget that ensures that the given Target
// installs exactly one snap. If the Target does not install exactly one snap,
// an error is returned.
func InstallOne(ctx context.Context, st *state.State, target Target, opts Options) (*snap.Info, *state.TaskSet, error) {
	infos, tasksets, err := InstallTarget(ctx, st, target, opts)
	if err != nil {
		return nil, nil, err
	}

	if len(infos) != 1 || len(tasksets) != 1 {
		return nil, nil, errors.New("internal error: expected exactly one snap and taskset")
	}

	return infos[0], tasksets[0], nil
}

// InstallTarget installs the snap/set of snaps specified by the given Target.
//
// The Target controls what snaps should be installed and where to source the
// snaps from. The Options struct contains optional parameters that apply to the
// installation operation.
//
// A slice of snap.Info structs is returned for each snap that is being
// installed along with a slice of state.TaskSet structs that represent the
// tasks that are part of the installation operation for each snap.
func InstallTarget(ctx context.Context, st *state.State, target Target, opts Options) ([]*snap.Info, []*state.TaskSet, error) {
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

	if initer, ok := target.(OptionInitializer); ok {
		if err := initer.InitOptions(st, &opts); err != nil {
			return nil, nil, err
		}
	} else {
		if err := setDefaultSnapstateOptions(st, &opts); err != nil {
			return nil, nil, err
		}
	}

	snaps, err := All(st)
	if err != nil {
		return nil, nil, err
	}

	installables, err := target.Installables(ctx, st, snaps, opts)
	if err != nil {
		return nil, nil, err
	}

	// TODO: should this be a field on the targets?
	installInfos := make([]minimalInstallInfo, 0, len(installables))
	for _, target := range installables {
		installInfos = append(installInfos, installSnapInfo{target.Info})
	}

	if err = checkDiskSpace(st, "install", installInfos, opts.UserID, opts.PrereqTracker); err != nil {
		return nil, nil, err
	}

	tasksets := make([]*state.TaskSet, 0, len(installables))
	infos := make([]*snap.Info, 0, len(installables))
	for _, inst := range installables {
		if inst.Snap.SnapPath != "" && inst.Snap.DownloadInfo != nil {
			return nil, nil, errors.New("internal error: installable cannot specify both a path and a download info")
		}

		info := inst.Info

		if opts.Flags.RequireTypeBase && info.Type() != snap.TypeBase && info.Type() != snap.TypeOS {
			return nil, nil, fmt.Errorf("unexpected snap type %q, instead of 'base'", info.Type())
		}

		opts.PrereqTracker.Add(info)

		snapst, ok := snaps[info.InstanceName()]
		if !ok {
			snapst = &SnapState{}
		}

		flags, err := earlyChecks(st, snapst, info, opts.Flags)
		if err != nil {
			return nil, nil, err
		}

		providerContentAttrs := defaultProviderContentAttrs(st, info, opts.PrereqTracker)
		snapsup := &SnapSetup{
			Channel:      inst.Snap.Channel,
			DownloadInfo: inst.Snap.DownloadInfo,
			SnapPath:     inst.Snap.SnapPath,
			CohortKey:    inst.Snap.CohortKey,

			Base:               info.Base,
			Prereq:             getKeys(providerContentAttrs),
			PrereqContentAttrs: providerContentAttrs,
			UserID:             opts.UserID,
			Flags:              flags.ForSnapSetup(),
			SideInfo:           &info.SideInfo,
			Type:               info.Type(),
			Version:            info.Version,
			PlugsOnly:          len(info.Slots) == 0,
			InstanceKey:        info.InstanceKey,
			ExpectedProvenance: info.SnapProvenance,
		}

		var instFlags int
		if opts.Flags.SkipConfigure {
			instFlags |= skipConfigure
		}

		ts, err := doInstall(st, snapst, snapsup, instFlags, opts.FromChange, inUseFor(opts.DeviceCtx))
		if err != nil {
			return nil, nil, err
		}

		ts.JoinLane(generateLane(st, opts))

		tasksets = append(tasksets, ts)
		infos = append(infos, info)
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
	opts.DeviceCtx, err = DevicePastSeeding(st, opts.DeviceCtx)
	if err != nil {
		return err
	}

	return nil
}

// PathTarget represents a single snap to be installed from a path on disk.
type PathTarget struct {
	// Path is the path to the snap on disk.
	Path string
	// InstanceName is the name of the snap to install.
	InstanceName string
	// RevOpts contains options that apply to the installation of this snap.
	RevOpts RevisionOptions
	// SideInfo contains extra information about the snap.
	SideInfo *snap.SideInfo
}

// verify that StoreTarget implements the Target and OptionInitializer
// interfaces
var (
	_ Target            = &PathTarget{}
	_ OptionInitializer = &PathTarget{}
)

// NewPathTarget creates a new PathTarget from the given name, path, and side
// info.
func NewPathTarget(name, path string, si *snap.SideInfo, opts RevisionOptions) *PathTarget {
	return &PathTarget{
		InstanceName: name,
		Path:         path,
		RevOpts:      opts,
		SideInfo:     si,
	}
}

// InitOptions initializes the SnapstateOptions before the installation of the
// snaps. Implements the OptionInitializer interface.
func (p *PathTarget) InitOptions(st *state.State, opts *Options) error {
	var err error
	opts.DeviceCtx, err = DeviceCtxFromState(st, opts.DeviceCtx)
	if err != nil {
		return err
	}

	return nil
}

// Installables returns the data needed to setup the snap from disk.
func (p *PathTarget) Installables(ctx context.Context, st *state.State, installedSnaps map[string]*SnapState, opts Options) ([]Installable, error) {
	si := p.SideInfo

	if si.RealName == "" {
		return nil, fmt.Errorf("internal error: snap name to install %q not provided", p.Path)
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return nil, fmt.Errorf("internal error: snap id set to install %q but revision is unset", p.Path)
		}
	}

	if p.InstanceName == "" {
		p.InstanceName = si.RealName
	}

	if err := snap.ValidateInstanceName(p.InstanceName); err != nil {
		return nil, fmt.Errorf("invalid instance name: %v", err)
	}

	if err := validateRevisionOpts(p.RevOpts); err != nil {
		return nil, fmt.Errorf("invalid revision options for snap %q: %w", p.InstanceName, err)
	}

	if p.RevOpts.Revision.Set() && p.RevOpts.Revision != si.Revision {
		return nil, fmt.Errorf("cannot install local snap %q: %v != %v (revision mismatch)", p.InstanceName, p.RevOpts.Revision, si.Revision)
	}

	info, err := validatedInfoFromPathAndSideInfo(p.InstanceName, p.Path, si)
	if err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(p.InstanceName)
	if info.SnapName() != snapName {
		return nil, fmt.Errorf("cannot install snap %q, the name does not match the metadata %q", p.InstanceName, info.SnapName())
	}
	info.InstanceKey = instanceKey

	var trackingChannel string
	if snapst, ok := installedSnaps[p.InstanceName]; ok && snapst.IsInstalled() {
		trackingChannel = snapst.TrackingChannel
	}

	channel, err := resolveChannel(p.InstanceName, trackingChannel, p.RevOpts.Channel, opts.DeviceCtx)
	if err != nil {
		return nil, err
	}

	inst := Installable{
		Snap: &SnapSetup{
			SnapPath:  p.Path,
			Channel:   channel,
			CohortKey: p.RevOpts.CohortKey,
		},
		Info: info,
	}

	return []Installable{inst}, nil
}
