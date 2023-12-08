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
	if err := st.Get("seeded", &seeded); err != nil && !errors.Is(err, state.ErrNoState) {
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
	_, sys, err := loadSeedAndSystem(label, current)
	return sys, err
}

func loadSeedAndSystem(label string, current *currentSystem) (seed.Seed, *System, error) {
	s, err := seedOpen(dirs.SnapSeedDir, label)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open: %v", err)
	}
	if err := s.LoadAssertions(nil, nil); err != nil {
		return nil, nil, fmt.Errorf("cannot load assertions for label %q: %v", label, err)
	}
	// get the model
	model := s.Model()
	brand, err := s.Brand()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot obtain brand: %v", err)
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
	// seeded systems are prepended to the list, so the most recently seeded
	// one comes first
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

// getInfoFunc is expected to return for a given snap name a snap.Info for that
// snap and whether the snap is present is present. The second bit is relevant
// for non-essential snaps mentioned in the model, which if present and having
// an 'optional' presence in the model, will be added to the recovery system.
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
	w, err := seedwriter.New(model, wOpts)
	if err != nil {
		return "", err
	}

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
		info, snapPath, present, err := getInfo(name)
		if err != nil {
			return fmt.Errorf("cannot obtain %v snap information: %v", kind, err)
		}
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
		if err := getModelSnap(sn.SnapName(), essential, ""); err != nil {
			return "", err
		}
	}
	// snapd is implicitly needed
	const snapdIsEssential = true
	if err := getModelSnap("snapd", snapdIsEssential, ""); err != nil {
		return "", err
	}
	for _, sn := range model.SnapsWithoutEssential() {
		const essential = false
		if err := getModelSnap(sn.SnapName(), essential, sn.Presence); err != nil {
			return "", err
		}
	}
	if err := w.SetOptionsSnaps(optsSnaps); err != nil {
		return "", err
	}

	newFetcher := func(save func(asserts.Assertion) error) asserts.Fetcher {
		fromDB := func(ref *asserts.Ref) (asserts.Assertion, error) {
			return ref.Resolve(db.Find)
		}

		seqFromDB := func(ref *asserts.AtSequence) (asserts.Assertion, error) {
			if ref.Sequence <= 0 {
				hdrs, err := asserts.HeadersFromSequenceKey(ref.Type, ref.SequenceKey)
				if err != nil {
					return nil, err
				}
				return db.FindSequence(ref.Type, hdrs, -1, -1)
			}
			return ref.Resolve(db.Find)
		}

		return asserts.NewSequenceFormingFetcher(db, fromDB, seqFromDB, save)
	}

	sf := seedwriter.MakeSeedAssertionFetcher(newFetcher)
	if err := w.Start(db, sf); err != nil {
		return "", err
	}
	// past this point the system directory is present

	localSnaps, err := w.LocalSnaps()
	if err != nil {
		return recoverySystemDir, err
	}

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
		_, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, model, sf, db)
		if err != nil {
			if !errors.Is(err, &asserts.NotFoundError{}) {
				return recoverySystemDir, err
			} else if info.SnapID != "" {
				// snap info from state must have come
				// from the store, so it is unexpected
				// if no assertions for it were found
				return recoverySystemDir, fmt.Errorf("internal error: no assertions for asserted snap with ID: %v", info.SnapID)
			}
		}
		if err := w.SetInfo(sn, info); err != nil {
			return recoverySystemDir, err
		}
		localARefs[sn] = aRefs
	}

	if err := w.InfoDerived(); err != nil {
		return recoverySystemDir, err
	}

	retrieveAsserts := func(sn, _, _ *seedwriter.SeedSnap) ([]*asserts.Ref, error) {
		return localARefs[sn], nil
	}

	for {
		// get the list of snaps we need in this iteration
		toDownload, err := w.SnapsToDownload()
		if err != nil {
			return recoverySystemDir, err
		}
		// which should be empty as all snaps should be accounted for
		// already
		if len(toDownload) > 0 {
			which := make([]string, 0, len(toDownload))
			for _, sn := range toDownload {
				which = append(which, sn.SnapName())
			}
			return recoverySystemDir, fmt.Errorf("internal error: need to download snaps: %v", strings.Join(which, ", "))
		}

		complete, err := w.Downloaded(retrieveAsserts)
		if err != nil {
			return recoverySystemDir, err
		}
		if complete {
			logger.Debugf("snap processing for creating %q complete", label)
			break
		}
	}

	for _, warn := range w.Warnings() {
		logger.Noticef("WARNING creating system %q: %s", label, warn)
	}

	unassertedSnaps, err := w.UnassertedSnaps()
	if err != nil {
		return recoverySystemDir, err
	}
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
			if err := observeWrite(recoverySystemDir, dst); err != nil {
				return err
			}
		}
		return osutil.CopyFile(src, dst, 0)
	}
	if err := w.SeedSnaps(copySnap); err != nil {
		return recoverySystemDir, err
	}
	if err := w.WriteMeta(); err != nil {
		return recoverySystemDir, err
	}

	bootSnaps, err := w.BootSnaps()
	if err != nil {
		return recoverySystemDir, err
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
	if err := boot.MakeRecoverySystemBootable(model, boot.InitramfsUbuntuSeedDir, recoverySystemDirInRootDir, bootWith); err != nil {
		return recoverySystemDir, fmt.Errorf("cannot make candidate recovery system %q bootable: %v", label, err)
	}
	logger.Noticef("created recovery system %q", label)

	return recoverySystemDir, nil
}
