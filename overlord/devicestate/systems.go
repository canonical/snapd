// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	// "github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

func checkSystemRequestConflict(st *state.State, systemLabel string) error {
	st.Lock()
	defer st.Unlock()

	var seeded bool
	if err := st.Get("seeded", &seeded); err != nil && err != state.ErrNoState {
		return err
	}
	if seeded {
		// the system is fully seeded already
		return nil
	}

	// inspect the current system which is stored in modeenv, note we are
	// holding the state lock so there is no race against mark-seeded
	// clearing recovery system; recovery system is not cleared when seeding
	// fails
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		// non UC20 systems do not support actions, no conflict can
		// happen
		return nil
	}

	// not yet fully seeded, hold off requests for the system that is being
	// seeded, but allow requests for other systems
	if modeEnv.RecoverySystem == systemLabel {
		return &snapstate.ChangeConflictError{
			ChangeKind: "seed",
			Message:    "cannot request system action, system is seeding",
		}
	}
	return nil
}

func systemFromSeed(label string, current *currentSystem) (*System, error) {
	s, err := seed.Open(dirs.SnapSeedDir, label)
	if err != nil {
		return nil, fmt.Errorf("cannot open: %v", err)
	}
	if err := s.LoadAssertions(nil, nil); err != nil {
		return nil, fmt.Errorf("cannot load assertions: %v", err)
	}
	// get the model
	model := s.Model()
	brand, err := s.Brand()
	if err != nil {
		return nil, fmt.Errorf("cannot obtain brand: %v", err)
	}
	system := &System{
		Current: false,
		Label:   label,
		Model:   model,
		Brand:   brand,
		Actions: defaultSystemActions,
	}
	if current.sameAs(system) {
		system.Current = true
		system.Actions = current.actions
	}
	return system, nil
}

type currentSystem struct {
	*seededSystem
	actions []SystemAction
}

func (c *currentSystem) sameAs(other *System) bool {
	return c != nil &&
		c.System == other.Label &&
		c.Model == other.Model.Model() &&
		c.BrandID == other.Brand.AccountID()
}

func currentSystemForMode(st *state.State, mode string) (*currentSystem, error) {
	var system *seededSystem
	var actions []SystemAction
	var err error

	switch mode {
	case "run":
		actions = currentSystemActions
		system, err = currentSeededSystem(st)
	case "install":
		// there is no current system for install mode
		return nil, nil
	case "recover":
		actions = recoverSystemActions
		// recover mode uses modeenv for reference
		system, err = seededSystemFromModeenv()
	default:
		return nil, fmt.Errorf("internal error: cannot identify current system for unsupported mode %q", mode)
	}
	if err != nil {
		return nil, err
	}
	currentSys := &currentSystem{
		seededSystem: system,
		actions:      actions,
	}
	return currentSys, nil
}

func currentSeededSystem(st *state.State) (*seededSystem, error) {
	st.Lock()
	defer st.Unlock()

	var whatseeded []seededSystem
	if err := st.Get("seeded-systems", &whatseeded); err != nil {
		return nil, err
	}
	if len(whatseeded) == 0 {
		// unexpected
		return nil, state.ErrNoState
	}
	return &whatseeded[0], nil
}

func seededSystemFromModeenv() (*seededSystem, error) {
	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return nil, err
	}
	if modeEnv == nil {
		return nil, fmt.Errorf("internal error: modeenv does not exist")
	}
	if modeEnv.RecoverySystem == "" {
		return nil, fmt.Errorf("internal error: recovery system is unset")
	}

	system, err := systemFromSeed(modeEnv.RecoverySystem, nil)
	if err != nil {
		return nil, err
	}
	seededSys := &seededSystem{
		System:    modeEnv.RecoverySystem,
		Model:     system.Model.Model(),
		BrandID:   system.Model.BrandID(),
		Revision:  system.Model.Revision(),
		Timestamp: system.Model.Timestamp(),
		// SeedTime is intentionally left unset
	}
	return seededSys, nil
}

func createSystemForModelFromValidatedSnaps(getInfo func(name string) (*snap.Info, bool, error), db asserts.RODatabase, label string, model *asserts.Model) (newFiles []string, dir string, err error) {
	if isUC20 := model.Grade() != asserts.ModelGradeUnset; !isUC20 {
		return nil, "", fmt.Errorf("cannot create a system for non UC20 model")
	}

	logger.Noticef("creating recovery system with label %q for %q", label, model.Model())

	wOpts := &seedwriter.Options{
		// RW mount of ubuntu-seed
		SeedDir: boot.InitramfsUbuntuSeedDir,
		Label:   label,
	}
	w, err := seedwriter.New(model, wOpts)
	if err != nil {
		return nil, "", err
	}

	optsSnaps := make([]*seedwriter.OptionsSnap, 0, len(model.RequiredWithEssentialSnaps()))

	// collect all snaps that are present
	modelSnaps := make(map[string]*snap.Info)
	for _, sn := range model.EssentialSnaps() {
		info, present, err := getInfo(sn.SnapName())
		if err != nil {
			return nil, "", fmt.Errorf("cannot obtain snap information: %v", err)
		}
		// grab those
		logger.Noticef("essential snap: %v", sn.SnapName())
		if !present {
			return nil, "", fmt.Errorf("internal error: essential snap %q not present", sn.SnapName())
		}
		// present locally
		optsSnaps = append(optsSnaps, &seedwriter.OptionsSnap{
			Path: info.MountFile(),
		})
		modelSnaps[info.MountFile()] = info
	}
	for _, sn := range model.SnapsWithoutEssential() {
		info, present, err := getInfo(sn.SnapName())
		if err != nil {
			return nil, "", fmt.Errorf("cannot obtain non-essential snap information: %v", err)
		}
		logger.Noticef("non-essential but %q snap %v", sn.Presence, sn.SnapName())
		if sn.Presence == "optional" && !present {
			// the snap is optional
			continue
		}
		if !present {
			return nil, "", fmt.Errorf("internal error: non-essential but %q snap %q not present", sn.Presence, sn.SnapName())
		}
		// present locally
		optsSnaps = append(optsSnaps, &seedwriter.OptionsSnap{
			Path: info.MountFile(),
		})
		modelSnaps[info.MountFile()] = info
	}
	if err := w.SetOptionsSnaps(optsSnaps); err != nil {
		return nil, "", err
	}

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		fromDB := func(ref *asserts.Ref) (asserts.Assertion, error) {
			return ref.Resolve(db.Find)
		}
		return asserts.NewFetcher(db, fromDB, save)
	}
	f, err := w.Start(db, newFetcher)
	if err != nil {
		return nil, "", err
	}

	localSnaps, err := w.LocalSnaps()
	if err != nil {
		return nil, "", err
	}

	for _, sn := range localSnaps {
		// TODO: is the side info here different from what we have in snap.Info?
		_, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, f, db)
		if err != nil && !asserts.IsNotFound(err) {
			return nil, "", err
		}
		info, ok := modelSnaps[sn.Path]
		if !ok {
			return nil, "", fmt.Errorf("internal error: no snap info for %q", sn.SnapName())
		}
		if err := w.SetInfo(sn, info); err != nil {
			return nil, "", err
		}
		sn.ARefs = aRefs
	}

	if err := w.InfoDerived(); err != nil {
		return nil, "", err
	}

	for {
		// get the list of snaps we need in this iteration
		toDownload, err := w.SnapsToDownload()
		if err != nil {
			return nil, "", err
		}
		// which should be empty as all snaps should be accounted for
		// already
		if len(toDownload) > 0 {
			which := make([]string, 0, len(toDownload))
			for _, sn := range toDownload {
				which = append(which, sn.SnapName())
			}
			return nil, "", fmt.Errorf("internal error: need to download snaps: %v", strings.Join(which, ", "))
		}

		complete, err := w.Downloaded()
		if err != nil {
			return nil, "", err
		}
		if complete {
			logger.Noticef("snap processing complete")
			break
		}
	}

	for _, warn := range w.Warnings() {
		logger.Noticef("WARNING: %s", warn)
	}

	unassertedSnaps, err := w.UnassertedSnaps()
	if err != nil {
		return nil, "", err
	}
	if len(unassertedSnaps) > 0 {
		locals := make([]string, len(unassertedSnaps))
		for i, sn := range unassertedSnaps {
			locals[i] = sn.SnapName()
		}
		logger.Noticef("system %q contains unasserted snaps %s", label, strutil.Quoted(locals))
	}

	// TODO: should that path be built in boot instead?
	recoverySystemDirInRootDir := filepath.Join("/systems", label)
	recoverySystemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, recoverySystemDirInRootDir)

	copySnap := func(name, src, dst string) error {
		logger.Noticef("copying seed snap %q from %v to %v", name, src, dst)
		// XXX: should not overwrite, check whether the file exists,
		// track the new ones that are added
		if !osutil.FileExists(dst) {
			newFiles = append(newFiles, dst)
		}
		return osutil.CopyFile(src, dst, osutil.CopyFlagOverwrite)
	}
	if err := w.SeedSnaps(copySnap); err != nil {
		return newFiles, recoverySystemDir, err
	}
	if err := w.WriteMeta(); err != nil {
		return newFiles, recoverySystemDir, err
	}

	bootSnaps, err := w.BootSnaps()
	if err != nil {
		return newFiles, recoverySystemDir, err
	}
	bootWith := &boot.RecoverySystemBootableSet{}
	for _, sn := range bootSnaps {
		switch sn.Info.Type() {
		case snap.TypeKernel:
			bootWith.Kernel = sn.Info
			bootWith.KernelPath = sn.Path
		case snap.TypeGadget:
			bootWith.GadgetSnapOrDir = sn.Path
		}
	}
	if err := boot.MakeRecoverySystemBootable(boot.InitramfsUbuntuSeedDir, recoverySystemDirInRootDir, bootWith); err != nil {
		return newFiles, recoverySystemDir, fmt.Errorf("cannot make candidate recovery system %q bootable: %v", label, err)
	}
	logger.Noticef("all done")

	return newFiles, recoverySystemDir, nil
}
