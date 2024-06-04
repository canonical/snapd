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

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

func isSameAssertsRevision(err error) bool {
	if e, ok := err.(*asserts.RevisionError); ok {
		if e.Used == e.Current {
			return true
		}
	}
	return false
}

var injectedSetModelError error

// InjectSetModelError will trigger the selected error in the doSetModel
// handler. This is only useful for testing.
func InjectSetModelError(err error) {
	injectedSetModelError = err
}

func (m *DeviceManager) doSetModel(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodCtx, err := remodelCtxFromTask(t)
	if err != nil {
		return err
	}
	new := remodCtx.Model()

	// unmark no-longer required snaps
	var cleanedRequiredSnaps []string
	requiredSnaps := getAllRequiredSnapsForModel(new)
	// TODO|XXX: have AllByRef
	snapStates, err := snapstate.All(st)
	if err != nil {
		return err
	}
	for snapName, snapst := range snapStates {
		// TODO: remove this type restriction once we remodel
		//       gadgets and add tests that ensure
		//       that the required flag is properly set/unset
		typ, err := snapst.Type()
		if err != nil {
			return err
		}
		if typ != snap.TypeApp && typ != snap.TypeBase && typ != snap.TypeKernel {
			continue
		}
		// clean required flag if no-longer needed
		if snapst.Flags.Required && !requiredSnaps.Contains(naming.Snap(snapName)) {
			snapst.Flags.Required = false
			snapstate.Set(st, snapName, snapst)
			cleanedRequiredSnaps = append(cleanedRequiredSnaps, snapName)
		}
		// TODO: clean "required" flag of "core" if a remodel
		//       moves from the "core" snap to a different
		//       bootable base snap.
	}
	// ensure  we undo the cleanedRequiredSnaps if e.g. remodCtx
	defer func() {
		if err == nil {
			return
		}
		var snapst snapstate.SnapState
		for _, snapName := range cleanedRequiredSnaps {
			if err := snapstate.Get(st, snapName, &snapst); err == nil {
				snapst.Flags.Required = true
				snapstate.Set(st, snapName, &snapst)
			}
		}
	}()

	currentSets, err := trackedValidationSetsFromModel(st, remodCtx.GroundContext().Model())

	for _, old := range currentSets {
		if err := assertstate.ForgetValidationSet(st, old.AccountID(), old.Name(), assertstate.ForgetValidationSetOpts{
			ForceForget: true,
		}); err != nil {
			return err
		}
	}

	newSets := new.ValidationSets()

	defer func() {
		if err == nil {
			return
		}

		// restore the old validation sets if something went wrong
		if err := rollBackValidationSets(st, currentSets, newSets, remodCtx); err != nil {
			logger.Debugf("cannot rollback validation sets: %v", err)
		}
	}()

	if err := enforceValidationSetsForRemodel(st, newSets); err != nil {
		return err
	}

	// only useful for testing
	if injectedSetModelError != nil {
		return injectedSetModelError
	}

	// add the assertion only after everything else was successful
	err = assertstate.Add(st, new)
	if err != nil && !isSameAssertsRevision(err) {
		return err
	}

	// hybrid core/classic systems might have a system-seed-null; in that case,
	// we cannot create a recovery system
	hasSystemSeed, err := checkForSystemSeed(st, remodCtx)
	if err != nil {
		return fmt.Errorf("cannot find ubuntu seed role: %w", err)
	}

	var recoverySetup *recoverySystemSetup
	if new.Grade() != asserts.ModelGradeUnset && hasSystemSeed {
		var triedSystems []string
		if err := st.Get("tried-systems", &triedSystems); err != nil {
			return fmt.Errorf("cannot obtain tried recovery systems: %v", err)
		}
		recoverySetup, err = taskRecoverySystemSetup(t)
		if err != nil {
			return err
		}
		// should promoting or any of the later steps fails, the cleanup
		// will be done in finalize-recovery-system undo
		if err := boot.PromoteTriedRecoverySystem(remodCtx, recoverySetup.Label, triedSystems); err != nil {
			return err
		}
		remodCtx.setRecoverySystemLabel(recoverySetup.Label)
	}

	logEverywhere := func(format string, args ...interface{}) {
		t.Logf(format, args)
		logger.Noticef(format, args)
	}

	// and finish (this will set the new model), note that changes done in
	// here are not recoverable even if an error occurs
	if err := remodCtx.Finish(); err != nil {
		logEverywhere("cannot complete remodel: %v", err)
	}

	t.SetStatus(state.DoneStatus)

	return nil
}

func trackedValidationSetsFromModel(st *state.State, model *asserts.Model) ([]*asserts.ValidationSet, error) {
	currentSets, err := assertstate.TrackedEnforcedValidationSets(st)
	if err != nil {
		return nil, err
	}

	var fromModel []*asserts.ValidationSet
	for _, mvs := range model.ValidationSets() {
		for _, cvs := range currentSets.Sets() {
			if mvs.SequenceKey() == cvs.SequenceKey() {
				fromModel = append(fromModel, cvs)
			}
		}
	}
	return fromModel, nil
}

func rollBackValidationSets(st *state.State, oldSets []*asserts.ValidationSet, newSets []*asserts.ModelValidationSet, deviceCtx snapstate.DeviceContext) error {
	for _, set := range newSets {
		if err := assertstate.ForgetValidationSet(st, set.AccountID, set.Name, assertstate.ForgetValidationSetOpts{
			ForceForget: true,
		}); err != nil {
			return err
		}
	}

	vSetKeys := make(map[string][]string, len(oldSets))
	for _, vs := range oldSets {
		sequenceName := vs.SequenceKey()
		vSetKeys[sequenceName] = vs.At().PrimaryKey
	}

	snaps, ignore, err := snapstate.InstalledSnaps(st)
	if err != nil {
		return err
	}

	// we must ignore all snaps that are currently installed, since those snaps
	// were installed in accordance to the new model and validation sets.
	//
	// alternatively, a more complex (but potentially more robust) approach
	// would be to split logic for undoing the validation sets and applying the
	// validation sets into different tasks. then, we can put the undo task
	// early in the change. this would allow us to undo the validation sets
	// after the snap installations/refreshes have been undone.
	for _, sn := range snaps {
		ignore[sn.SnapName()] = true
	}

	if err := assertstate.ApplyLocalEnforcedValidationSets(st, vSetKeys, nil, snaps, ignore); err != nil {
		return err
	}

	return nil
}

func resolveValidationSetAssertion(seq *asserts.AtSequence, db asserts.RODatabase) (asserts.Assertion, error) {
	if seq.Sequence <= 0 {
		hdrs, err := asserts.HeadersFromSequenceKey(seq.Type, seq.SequenceKey)
		if err != nil {
			return nil, err
		}
		return db.FindSequence(seq.Type, hdrs, -1, seq.Type.MaxSupportedFormat())
	}
	return seq.Resolve(db.Find)
}

func enforceValidationSetsForRemodel(st *state.State, sets []*asserts.ModelValidationSet) error {
	vsPrimaryKeys := make(map[string][]string, len(sets))
	db := assertstate.DB(st)
	for _, vs := range sets {
		a, err := resolveValidationSetAssertion(vs.AtSequence(), db)
		if err != nil {
			return err
		}
		vsPrimaryKeys[vs.SequenceKey()] = a.At().PrimaryKey
	}

	pinnedValidationSeqs := make(map[string]int, len(sets))
	for _, vs := range sets {
		if vs.Sequence > 0 {
			pinnedValidationSeqs[vs.SequenceKey()] = vs.Sequence
		}
	}

	snaps, ignoreValidation, err := snapstate.InstalledSnaps(st)
	if err != nil {
		return fmt.Errorf("cannot list installed snaps for validation: %w", err)
	}

	// validation sets should already be downloaded, so we can use the local
	// version of this function
	if err := assertstate.ApplyLocalEnforcedValidationSets(st, vsPrimaryKeys, pinnedValidationSeqs, snaps, ignoreValidation); err != nil {
		return fmt.Errorf("cannot enforce validation sets: %v", err)
	}
	return nil
}

func (m *DeviceManager) cleanupRemodel(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	// cleanup the cached remodel context
	cleanupRemodelCtx(t.Change())
	return nil
}

func (m *DeviceManager) doPrepareRemodeling(t *state.Task, tmb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodCtx, err := remodelCtxFromTask(t)
	if err != nil {
		return err
	}
	current, err := findModel(st)
	if err != nil {
		return err
	}

	sto := remodCtx.Store()
	if sto == nil {
		return fmt.Errorf("internal error: re-registration remodeling should have built a store")
	}
	// ensure a new session accounting for the new brand/model
	st.Unlock()
	err = sto.EnsureDeviceSession()
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot get a store session based on the new model assertion: %v", err)
	}

	chgID := t.Change().ID()

	tss, err := remodelTasks(tmb.Context(nil), st, current, remodCtx.Model(), remodCtx, chgID, nil, nil, RemodelOptions{})
	if err != nil {
		return err
	}

	allTs := state.NewTaskSet()
	for _, ts := range tss {
		allTs.AddAll(ts)
	}
	snapstate.InjectTasks(t, allTs)

	st.EnsureBefore(0)
	t.SetStatus(state.DoneStatus)

	return nil
}

var (
	gadgetIsCompatible = gadget.IsCompatible
)

func checkGadgetRemodelCompatible(st *state.State, snapInfo, curInfo *snap.Info, snapf snap.Container, flags snapstate.Flags, deviceCtx snapstate.DeviceContext) error {
	if release.OnClassic {
		return nil
	}
	if snapInfo.Type() != snap.TypeGadget {
		// We are only interested in gadget snaps.
		return nil
	}
	if deviceCtx == nil || !deviceCtx.ForRemodeling() {
		// We are only interesting in a remodeling scenario.
		return nil
	}

	if curInfo == nil {
		// snap isn't installed yet, we are likely remodeling to a new
		// gadget, identify the old gadget
		curInfo, _ = snapstate.GadgetInfo(st, deviceCtx.GroundContext())
	}
	if curInfo == nil {
		return fmt.Errorf("cannot identify the current gadget snap")
	}

	pendingInfo, err := gadget.ReadInfoFromSnapFile(snapf, deviceCtx.Model())
	if err != nil {
		return fmt.Errorf("cannot read new gadget metadata: %v", err)
	}

	currentData, err := gadgetDataFromInfo(curInfo, deviceCtx.GroundContext().Model())
	if err != nil {
		return fmt.Errorf("cannot read current gadget metadata: %v", err)
	}

	if err := gadgetIsCompatible(currentData.Info, pendingInfo); err != nil {
		return fmt.Errorf("cannot remodel to an incompatible gadget: %v", err)
	}
	return nil
}
