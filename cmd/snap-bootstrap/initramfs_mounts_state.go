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

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/integrity"
	"github.com/snapcore/snapd/timings"
)

var (
	osutilSetTime                   = osutil.SetTime
	runtimeNumCPU                   = runtime.NumCPU
	lookupDmVerityDataAndCrossCheck = integrity.LookupDmVerityDataAndCrossCheck
)

// initramfsMountsState helps tracking the state and progress
// of the mounting driving process.
type initramfsMountsState struct {
	mode           string
	recoverySystem string

	verifiedModel gadget.Model
	seeds         map[string]seed.Seed

	activateContext secboot.ActivateContext
}

var errRunModeNoImpliedRecoverySystem = errors.New("internal error: no implied recovery system in run mode")

// LoadSeed returns the seed for the recoverySystem.
// If recoverySystem is "" the implied one will be used (only for
// modes other than run).
func (mst *initramfsMountsState) LoadSeed(recoverySystem string) (seed.Seed, error) {
	if recoverySystem == "" {
		if mst.mode == "run" {
			return nil, errRunModeNoImpliedRecoverySystem
		}
		recoverySystem = mst.recoverySystem
	}

	if mst.seeds == nil {
		mst.seeds = make(map[string]seed.Seed)
	}
	foundSeed, hasSeed := mst.seeds[recoverySystem]
	if hasSeed {
		return foundSeed, nil
	}

	perf := timings.New(nil)

	// get the current time to pass to ReadSeedAndBetterEarliestTime
	// note that we trust the time we have from the system, because that time
	// comes from either:
	// * a RTC on the system that the kernel/systemd consulted and used to move
	//   time forward
	// * systemd using a built-in timestamp from the initrd which was stamped
	//   when the initrd was built, giving a lower bound on the current time if
	//   the RTC does not have a battery or is otherwise unreliable, etc.
	now := timeNow()

	jobs := 1
	if runtimeNumCPU() > 1 {
		jobs = 2
	}
	seed20, newTrustedEarliestTime, err := seed.ReadSeedAndBetterEarliestTime(boot.InitramfsUbuntuSeedDir, recoverySystem, now, jobs, perf)
	if err != nil {
		return nil, err
	}

	// set the time on the system to move forward if it is in the future - never
	// move the time backwards
	if newTrustedEarliestTime.After(now) {
		if err := osutilSetTime(newTrustedEarliestTime); err != nil {
			// log the error but don't fail on it, we should be able to continue
			// even if the time can't be moved forward
			logger.Noticef("failed to move time forward from %s to %s: %v", now, newTrustedEarliestTime, err)
		}
	}

	mst.seeds[recoverySystem] = seed20

	return seed20, nil
}

// SetVerifiedBootModel sets the "verifiedModel" field. It should only
// be called after the model is verified. Either via a successful unlock
// of the encrypted data or after validating the seed in install/recover
// mode.
func (mst *initramfsMountsState) SetVerifiedBootModel(m gadget.Model) {
	mst.verifiedModel = m
}

// UnverifiedBootModel returns the unverified model from the
// boot partition for run mode. The current and only use case
// is measuring the model for run mode. Otherwise no decisions
// should be based on an unverified model. Note that the model
// is verified at the time the key auth policy is computed.
func (mst *initramfsMountsState) UnverifiedBootModel() (*asserts.Model, error) {
	if mst.mode != "run" {
		return nil, fmt.Errorf("internal error: unverified boot model access is for limited run mode use")
	}

	mf, err := os.Open(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return nil, fmt.Errorf("cannot read model assertion: %v", err)
	}
	defer mf.Close()
	ma, err := asserts.NewDecoder(mf).Decode()
	if err != nil {
		return nil, fmt.Errorf("cannot decode assertion: %v", err)
	}
	if ma.Type() != asserts.ModelType {
		return nil, fmt.Errorf("unexpected assertion: %q", ma.Type().Name)
	}
	return ma.(*asserts.Model), nil
}

// EphemeralModeenvForModel generates a modeenv given the model and the snaps for the
// current mode and recovery system of the initramfsMountsState.
func (mst *initramfsMountsState) EphemeralModeenvForModel(model *asserts.Model, snaps map[snap.Type]*seed.Snap) (*boot.Modeenv, error) {
	if mst.mode == "run" {
		return nil, fmt.Errorf("internal error: initramfs should not write modeenv in run mode")
	}
	return &boot.Modeenv{
		Mode:           mst.mode,
		RecoverySystem: mst.recoverySystem,
		Base:           snaps[snap.TypeBase].PlaceInfo().Filename(),
		Gadget:         snaps[snap.TypeGadget].PlaceInfo().Filename(),
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		// TODO:UC20: what about current kernel snaps, trusted boot assets and
		//            kernel command lines?
	}, nil
}

// LoadEssentialSnapRevisions will load the snap-revision assertions for the essential snaps that are going to be used
// in this boot. During the first boot in run mode, the assertion database hasn't yet been created therefore the
// snap-revision assertions from the seed are being used. Otherwise an in-memory assertion database is instantiated from
// the assertion db in ubuntu-data.
func (mst *initramfsMountsState) LoadEssentialSnapRevisions(modeEnv *boot.Modeenv, mounts map[snap.Type]snap.PlaceInfo) error {

	if mst.essentialSnaps == nil {
		mst.essentialSnaps = make(map[string]*essentialSnapInfo)
	}

	isRunMode := true
	rootfsDir := boot.InitramfsWritableDir(mst.verifiedModel, isRunMode)
	assertionsPath := dirs.SnapAssertionsDirUnder(rootfsDir)

	if mst.firstBoot {
		theSeed, err := mst.LoadSeed(modeEnv.RecoverySystem)
		if err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}
		perf := timings.New(nil)
		if err := theSeed.LoadEssentialMeta([]snap.Type{snap.TypeBase, snap.TypeKernel, snap.TypeGadget}, perf); err != nil {
			return fmt.Errorf("cannot load metadata and verify snapd snap: %v", err)
		}

		essSnaps := theSeed.EssentialSnaps()

		for _, essSnap := range essSnaps {
			// we use the path of the run mode snap and the assertion from the seed
			snapPath := filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), essSnap.PlaceInfo().Filename())
			mst.essentialSnaps[essSnap.SnapName()] = &essentialSnapInfo{
				path:                snapPath,
				integrityDataParams: essSnap.IntegrityDataParams,
			}
		}

		return nil
	}

	// In order to avoid hashing each snap to get to the 'snap-sha3-384', which is the primary key for snap-revision
	// assertions, the latest snap-revision assertion for each snap is discovered from its name and the latest
	// snap revision number as it is parsed from the modeenv. The snap-revision assertion is then loaded in a
	// temporary in-memory database in order to be validated against the Canonical Root account-key. The asserts
	// Fetcher API will validate the snap-revision assertions prerequisite assertions too.
	fsdb, err := sysdb.OpenAt(assertionsPath)
	if err != nil {
		return err
	}

	// The snapRevisions found during this step are not verified against the assertion db.
	unverifiedSnapRevisionsMap := make(map[string]string)

	for _, sn := range mounts {
		decl, err := assertstate.SnapDeclarationFromNameAndAuthority(fsdb, sn.SnapName(), modeEnv.BrandID)
		if err != nil {
			return err
		}

		rev, err := assertstate.SnapRevisionFromSnapIdAndRevisionNumber(fsdb, decl.SnapID(), sn.SnapRevision().N)
		if err != nil {
			return err
		}

		unverifiedSnapRevisionsMap[rev.SnapSHA3_384()] = sn.SnapName()

		mst.essentialSnaps[sn.SnapName()] = &essentialSnapInfo{
			path:                filepath.Join(dirs.SnapBlobDirUnder(rootfsDir), sn.Filename()),
			integrityDataParams: nil,
		}
	}

	// create a temporary in-memory database bootstrapped with the Canonical Root key
	memdb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdbTrusted(),
	})
	if err != nil {
		return err
	}

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(fsdb.Find)
	}
	save := func(a asserts.Assertion) error {
		// for checking
		err := memdb.Add(a)
		if err != nil {
			if _, ok := err.(*asserts.RevisionError); ok {
				return nil
			}
			return fmt.Errorf("cannot add assertion %v: %v", a.Ref(), err)
		}

		if rev, ok := a.(*asserts.SnapRevision); ok {
			snapName := unverifiedSnapRevisionsMap[rev.SnapSHA3_384()]
			if snapName != "" {
				idp, err := integrity.NewIntegrityDataParamsFromRevision(rev)

				// Currently integrity data are not enforced therefore errors returned when integrity data
				// are not found for a snap revision are ignored.
				if err != nil && err != integrity.ErrNoIntegrityDataFoundInRevision {
					return err
				}

				// After validation, store the discovered revisions in the state to be
				// retrieved later during mount generation in order to retrieve integrity
				// data.
				mst.essentialSnaps[snapName].integrityDataParams = idp
			}
		}

		return nil
	}

	f := asserts.NewFetcher(memdb, retrieve, save)

	// Using the fetcher, the snap revisions that were discovered in the fs-backed assertion database
	// are read again from the disk and verified against the Canonical Root key.
	for revHash := range unverifiedSnapRevisionsMap {
		err := f.Fetch(&asserts.Ref{
			Type:       asserts.SnapRevisionType,
			PrimaryKey: []string{revHash},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

type essentialSnapInfo struct {
	path                string
	integrityDataParams *integrity.IntegrityDataParams
}

func (ess *essentialSnapInfo) GetVerityOptions() (*dmVerityOptions, error) {
	hashDevice, err := lookupDmVerityDataAndCrossCheck(ess.path, ess.integrityDataParams)

	if err != nil && err == integrity.ErrIntegrityDataParamsNotFound {
		// TODO: this should throw error if integrity data are required by
		// policy in the future
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("cannot generate mount for snap %s: %w", ess.path, err)
	}

	// TODO: we currently rely on several parameters from the on-disk unverified superblock
	// which gets automatically parsed by veritysetup for the mount. Instead we can use
	// the parameters we already have in the assertion as options to the mount but this
	// would require extra support in libmount.
	return &dmVerityOptions{
		HashDevice: hashDevice,
		RootHash:   ess.integrityDataParams.Digest,
	}, nil
}
