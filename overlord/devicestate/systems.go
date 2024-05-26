// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
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
	if mylog.Check(st.Get("seeded", &seeded)); err != nil && !errors.Is(err, state.ErrNoState) {
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
	modeEnv := mylog.Check2(maybeReadModeenv())

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

func systemFromSeed(label string, current *currentSystem, defaultRecoverySystem *DefaultRecoverySystem) (*System, error) {
	_, sys := mylog.Check3(loadSeedAndSystem(label, current, defaultRecoverySystem))
	return sys, err
}

func loadSeedAndSystem(label string, current *currentSystem, defaultRecoverySystem *DefaultRecoverySystem) (seed.Seed, *System, error) {
	s := mylog.Check2(seedOpen(dirs.SnapSeedDir, label))
	mylog.Check(s.LoadAssertions(nil, nil))

	// get the model
	model := s.Model()
	brand := mylog.Check2(s.Brand())

	system := &System{
		Current: false,
		Label:   label,
		Model:   model,
		Brand:   brand,
		Actions: defaultSystemActions,
	}
	system.DefaultRecoverySystem = defaultRecoverySystem.sameAs(system)
	if current.sameAs(system) {
		system.Current = true
		system.Actions = current.actions
	}
	return s, system, nil
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

	switch mode {
	case "run":
		actions = currentSystemActions
		system = mylog.Check2(currentSeededSystem(st))
	case "install":
		// there is no current system for install mode
		return nil, nil
	case "recover":
		actions = recoverSystemActions
		// recover mode uses modeenv for reference
		system = mylog.Check2(seededSystemFromModeenv())
	default:
		return nil, fmt.Errorf("internal error: cannot identify current system for unsupported mode %q", mode)
	}

	currentSys := &currentSystem{
		seededSystem: system,
		actions:      actions,
	}
	return currentSys, nil
}

func currentSeededSystem(st *state.State) (*seededSystem, error) {
	var whatseeded []seededSystem
	mylog.Check(st.Get("seeded-systems", &whatseeded))

	if len(whatseeded) == 0 {
		// unexpected
		return nil, state.ErrNoState
	}
	// seeded systems are prepended to the list, so the most recently seeded
	// one comes first
	return &whatseeded[0], nil
}

func seededSystemFromModeenv() (*seededSystem, error) {
	modeEnv := mylog.Check2(maybeReadModeenv())

	if modeEnv == nil {
		return nil, fmt.Errorf("internal error: modeenv does not exist")
	}
	if modeEnv.RecoverySystem == "" {
		return nil, fmt.Errorf("internal error: recovery system is unset")
	}

	system := mylog.Check2(systemFromSeed(modeEnv.RecoverySystem, nil, nil))

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

// getInfoFunc is expected to return for a given snap name a snap.Info for that
// snap, a path on disk where the snap file can be found, and whether the snap
// is present. The last bit is relevant for non-essential snaps mentioned in the
// model, which if present and having an 'optional' presence in the model, will
// be added to the recovery system.
type getSnapInfoFunc func(name string) (info *snap.Info, path string, snapIsPresent bool, err error)

// snapWriteObserveFunc is called with the recovery system directory and the
// path to a snap file being written. The snap file may be written to a location
// under the common snaps directory.
type snapWriteObserveFunc func(systemDir, where string) error

// createSystemForModelFromValidatedSnaps creates a new recovery system for the
// specified model with the specified label using the snaps in the database and
// the getInfo function.
//
// The function returns the directory of the new recovery system as well as the
// set of absolute file paths to the new snap files that were written for the
// recovery system - some snaps may be in the recovery system directory while
// others may be in the common snaps directory shared between multiple recovery
// systems on ubuntu-seed.
func createSystemForModelFromValidatedSnaps(model *asserts.Model, label string, db asserts.RODatabase, getInfo getSnapInfoFunc, observeWrite snapWriteObserveFunc) (dir string, err error) {
	if model.Grade() == asserts.ModelGradeUnset {
		return "", fmt.Errorf("cannot create a system for pre-UC20 model")
	}

	logger.Noticef("creating recovery system with label %q for %q", label, model.Model())

	// TODO: should that path provided by boot package instead?
	recoverySystemDirInRootDir := filepath.Join("/systems", label)
	assertedSnapsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps")
	recoverySystemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, recoverySystemDirInRootDir)

	wOpts := &seedwriter.Options{
		// RW mount of ubuntu-seed
		SeedDir: boot.InitramfsUbuntuSeedDir,
		Label:   label,
	}
	w := mylog.Check2(seedwriter.New(model, wOpts))

	optsSnaps := make([]*seedwriter.OptionsSnap, 0, len(model.RequiredWithEssentialSnaps()))
	// collect all snaps that are present
	modelSnaps := make(map[string]*snap.Info)

	getModelSnap := func(name string, essential bool, nonEssentialPresence string) error {
		kind := "essential"
		if !essential {
			kind = "non-essential"
			if nonEssentialPresence != "" {
				kind = fmt.Sprintf("non-essential but %v", nonEssentialPresence)
			}
		}
		info, snapPath, present := mylog.Check4(getInfo(name))

		if !essential && !present && nonEssentialPresence == "optional" {
			// non-essential snap which is declared as optionally
			// present in the model
			return nil
		}
		// grab those
		logger.Debugf("%v snap: %v", kind, name)
		if !present {
			return fmt.Errorf("internal error: %v snap %q not present", kind, name)
		}
		if _, ok := modelSnaps[snapPath]; ok {
			// we've already seen this snap
			return nil
		}
		// present locally
		// TODO: for grade dangerous we could have a channel here which is not
		//       the model channel, handle that here
		optsSnaps = append(optsSnaps, &seedwriter.OptionsSnap{
			Path: snapPath,
		})
		modelSnaps[snapPath] = info
		return nil
	}

	for _, sn := range model.EssentialSnaps() {
		const essential = true
		mylog.Check(getModelSnap(sn.SnapName(), essential, ""))

	}
	// snapd is implicitly needed
	const snapdIsEssential = true
	mylog.Check(getModelSnap("snapd", snapdIsEssential, ""))

	for _, sn := range model.SnapsWithoutEssential() {
		const essential = false
		mylog.Check(getModelSnap(sn.SnapName(), essential, sn.Presence))

	}
	mylog.Check(w.SetOptionsSnaps(optsSnaps))

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		fromDB := func(ref *asserts.Ref) (asserts.Assertion, error) {
			return ref.Resolve(db.Find)
		}

		seqFromDB := func(ref *asserts.AtSequence) (asserts.Assertion, error) {
			if ref.Sequence <= 0 {
				hdrs := mylog.Check2(asserts.HeadersFromSequenceKey(ref.Type, ref.SequenceKey))

				return db.FindSequence(ref.Type, hdrs, -1, -1)
			}
			return ref.Resolve(db.Find)
		}

		return asserts.NewSequenceFormingFetcher(db, fromDB, seqFromDB, save)
	}

	sf := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	mylog.Check(w.Start(db, sf))

	// past this point the system directory is present

	localSnaps := mylog.Check2(w.LocalSnaps())

	localARefs := make(map[*seedwriter.SeedSnap][]*asserts.Ref)
	for _, sn := range localSnaps {
		info, ok := modelSnaps[sn.Path]
		if !ok {
			return recoverySystemDir, fmt.Errorf("internal error: no snap info for %q", sn.Path)
		}
		// TODO: the side info derived here can be different from what
		// we have in snap.Info, but getting it this way can be
		// expensive as we need to compute the hash, try to find a
		// better way
		_, aRefs := mylog.Check3(seedwriter.DeriveSideInfo(sn.Path, model, sf, db))
		mylog.Check(

			// snap info from state must have come
			// from the store, so it is unexpected
			// if no assertions for it were found

			w.SetInfo(sn, info))

		localARefs[sn] = aRefs
	}
	mylog.Check(w.InfoDerived())

	retrieveAsserts := func(sn, _, _ *seedwriter.SeedSnap) ([]*asserts.Ref, error) {
		return localARefs[sn], nil
	}

	for {
		// get the list of snaps we need in this iteration
		toDownload := mylog.Check2(w.SnapsToDownload())

		// which should be empty as all snaps should be accounted for
		// already
		if len(toDownload) > 0 {
			which := make([]string, 0, len(toDownload))
			for _, sn := range toDownload {
				which = append(which, sn.SnapName())
			}
			return recoverySystemDir, fmt.Errorf("internal error: need to download snaps: %v", strings.Join(which, ", "))
		}

		complete := mylog.Check2(w.Downloaded(retrieveAsserts))

		if complete {
			logger.Debugf("snap processing for creating %q complete", label)
			break
		}
	}

	for _, warn := range w.Warnings() {
		logger.Noticef("WARNING creating system %q: %s", label, warn)
	}

	unassertedSnaps := mylog.Check2(w.UnassertedSnaps())

	if len(unassertedSnaps) > 0 {
		locals := make([]string, len(unassertedSnaps))
		for i, sn := range unassertedSnaps {
			locals[i] = sn.SnapName()
		}
		logger.Noticef("system %q contains unasserted snaps %s", label, strutil.Quoted(locals))
	}

	copySnap := func(name, src, dst string) error {
		// if the destination snap is in the asserted snaps dir and already
		// exists, we don't need to copy it since asserted snaps are shared
		if strings.HasPrefix(dst, assertedSnapsDir+"/") && osutil.FileExists(dst) {
			return nil
		}
		// otherwise, unasserted snaps are not shared, so even if the
		// destination already exists if it is not in the asserted snaps we
		// should copy it
		logger.Noticef("copying new seed snap %q from %v to %v", name, src, dst)
		if observeWrite != nil {
			mylog.Check(observeWrite(recoverySystemDir, dst))
		}
		return osutil.CopyFile(src, dst, 0)
	}
	mylog.Check(w.SeedSnaps(copySnap))
	mylog.Check(w.WriteMeta())

	bootSnaps := mylog.Check2(w.BootSnaps())

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
	mylog.Check(boot.MakeRecoverySystemBootable(model, boot.InitramfsUbuntuSeedDir, recoverySystemDirInRootDir, bootWith))

	logger.Noticef("created recovery system %q", label)

	return recoverySystemDir, nil
}
