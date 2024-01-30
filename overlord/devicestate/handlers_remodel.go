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

	// only useful for testing
	if injectedSetModelError != nil {
		return injectedSetModelError
	}

	// add the assertion only after everything else was successful
	err = assertstate.Add(st, new)
	if err != nil && !isSameAssertsRevision(err) {
		return err
	}

	var recoverySetup *recoverySystemSetup
	if new.Grade() != asserts.ModelGradeUnset {
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
