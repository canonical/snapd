// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2023 Canonical Ltd
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

package boot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

var (
	secbootPCRHandleOfSealedKey      = secboot.PCRHandleOfSealedKey
	secbootReleasePCRResourceHandles = secboot.ReleasePCRResourceHandles

	seedReadSystemEssential = seed.ReadSystemEssential
)

func MockSeedReadSystemEssential(f func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error)) (restore func()) {
	osutil.MustBeTestBinary("cannot mock seedReadSystemEssential in a non-test binary")
	old := seedReadSystemEssential
	seedReadSystemEssential = f
	return func() {
		seedReadSystemEssential = old
	}
}

// Hook functions setup by devicestate to support device-specific full
// disk encryption implementations. The state must be locked when these
// functions are called.
var (
	// HasFDESetupHook purpose is to detect if the target kernel has a
	// fde-setup-hook. If kernelInfo is nil the current kernel is checked
	// assuming it is representative` of the target one.
	HasFDESetupHook = func(kernelInfo *snap.Info) (bool, error) {
		return false, nil
	}
)

// MockResealKeyToModeenv is only useful in testing.
func MockResealKeyToModeenv(f func(rootdir string, modeenv *Modeenv, expectReseal bool, unlocker Unlocker) error) (restore func()) {
	osutil.MustBeTestBinary("resealKeyToModeenv only can be mocked in tests")
	old := resealKeyToModeenv
	resealKeyToModeenv = f
	return func() {
		resealKeyToModeenv = old
	}
}

// MockSealKeyToModeenvFlags is used for testing from other packages.
type MockSealKeyToModeenvFlags = sealKeyToModeenvFlags

// MockSealKeyToModeenv is used for testing from other packages.
func MockSealKeyToModeenv(f func(key, saveKey secboot.BootstrappedContainer, model *asserts.Model, modeenv *Modeenv, flags MockSealKeyToModeenvFlags) error) (restore func()) {
	old := sealKeyToModeenv
	sealKeyToModeenv = f
	return func() {
		sealKeyToModeenv = old
	}
}

type sealKeyToModeenvFlags struct {
	// HasFDESetupHook is true if the kernel has a fde-setup hook to use
	HasFDESetupHook bool
	// FactoryReset indicates that the sealing is happening during factory
	// reset.
	FactoryReset bool
	// SnapsDir is set to provide a non-default directory to find
	// run mode snaps in.
	SnapsDir string
	// SeedDir is the path where to find mounted seed with
	// essential snaps.
	SeedDir string
	// Unlocker is used unlock the snapd state for long operations
	StateUnlocker Unlocker
	// UseTokens disables key file to save key datas and instead uses tokens of key slots
	UseTokens bool
}

// sealKeyToModeenvImpl seals the supplied keys to the parameters specified
// in modeenv.
// It assumes to be invoked in install mode.
func sealKeyToModeenvImpl(key, saveKey secboot.BootstrappedContainer, model *asserts.Model, modeenv *Modeenv, flags sealKeyToModeenvFlags) error {
	if !isModeeenvLocked() {
		return fmt.Errorf("internal error: cannot seal without the modeenv lock")
	}

	// make sure relevant locations exist
	for _, p := range []string{
		InitramfsSeedEncryptionKeyDir,
		InitramfsBootEncryptionKeyDir,
		InstallHostFDEDataDir(model),
		InstallHostFDESaveDir,
	} {
		// XXX: should that be 0700 ?
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}

	method := device.SealingMethodTPM
	if flags.HasFDESetupHook {
		method = device.SealingMethodFDESetupHook
	} else {
		if flags.StateUnlocker != nil {
			relock := flags.StateUnlocker()
			defer relock()
		}
	}

	return sealKeyToModeenvForMethod(method, key, saveKey, model, modeenv, flags)
}

type SealKeyForBootChainsParams struct {
	// RunModeBootChains are the boot chains run key role
	RunModeBootChains []BootChain
	// RecoveryBootChainsForRunKey are the extra boot chains for
	// run+recover key role.
	RecoveryBootChainsForRunKey []BootChain
	// RecoveryBootChains are the boot chains for recover key role
	RecoveryBootChains []BootChain
	// RoleToBlName maps bootloader role to the name of its bootloader
	RoleToBlName map[bootloader.Role]string
	// FactoryReset...
	FactoryReset bool
	// UseTokens disables key file to save key datas and instead uses tokens of key slots
	UseTokens bool
	// InstallHostWritableDir...
	InstallHostWritableDir string
}

func sealKeyForBootChainsImpl(method device.SealingMethod, key, saveKey secboot.BootstrappedContainer, params *SealKeyForBootChainsParams) error {
	return fmt.Errorf("FDE manager backend was not built in")
}

var SealKeyForBootChains = sealKeyForBootChainsImpl

func sealKeyToModeenvForMethod(method device.SealingMethod, key, saveKey secboot.BootstrappedContainer, model *asserts.Model, modeenv *Modeenv, flags sealKeyToModeenvFlags) error {
	params := &SealKeyForBootChainsParams{
		FactoryReset:           flags.FactoryReset,
		UseTokens:              flags.UseTokens,
		InstallHostWritableDir: InstallHostWritableDir(model),
	}

	var tbl bootloader.TrustedAssetsBootloader
	var bl bootloader.Bootloader
	if method != device.SealingMethodFDESetupHook {
		// build the recovery mode boot chain
		rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
			Role: bootloader.RoleRecovery,
		})
		if err != nil {
			return fmt.Errorf("cannot find the recovery bootloader: %v", err)
		}
		var ok bool
		tbl, ok = rbl.(bootloader.TrustedAssetsBootloader)
		if !ok {
			// TODO:UC20: later the exact kind of bootloaders we expect here might change
			return fmt.Errorf("internal error: cannot seal keys without a trusted assets bootloader")
		}

		// build the run mode boot chains
		bl, err = bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
			Role:        bootloader.RoleRunMode,
			NoSlashBoot: true,
		})
		if err != nil {
			return fmt.Errorf("cannot find the bootloader: %v", err)
		}
	}

	includeTryModel := false
	systems := []string{modeenv.RecoverySystem}
	modes := map[string][]string{
		// the system we are installing from is considered current and
		// tested, hence allow both recover and factory reset modes
		modeenv.RecoverySystem: {ModeRecover, ModeFactoryReset},
	}
	var err error
	params.RecoveryBootChains, err = recoveryBootChainsForSystems(systems, modes, tbl, modeenv, includeTryModel, flags.SeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}
	logger.Debugf("recovery bootchain:\n%+v", params.RecoveryBootChains)

	// kernel command lines are filled during install
	cmdlines := modeenv.CurrentKernelCommandLines
	params.RunModeBootChains, err = runModeBootChains(tbl, bl, modeenv, cmdlines, flags.SnapsDir)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}
	logger.Debugf("run mode bootchain:\n%+v", params.RunModeBootChains)

	params.RoleToBlName = make(map[bootloader.Role]string)
	if tbl != nil {
		params.RoleToBlName[bootloader.RoleRecovery] = tbl.Name()
	}
	if bl != nil {
		params.RoleToBlName[bootloader.RoleRunMode] = bl.Name()
	}

	return SealKeyForBootChains(method, key, saveKey, params)
}

func UsesAltPCRHandles() (bool, error) {
	saveFallbackKey := device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	// inspect the PCR handle of the ubuntu-save fallback key
	handle, err := secbootPCRHandleOfSealedKey(saveFallbackKey)
	if err != nil {
		return false, err
	}
	logger.Noticef("fallback sealed key %v PCR handle: %#x", saveFallbackKey, handle)
	return handle == secboot.AltFallbackObjectPCRPolicyCounterHandle, nil
}

var resealKeyToModeenv = resealKeyToModeenvImpl

// resealKeyToModeenv reseals the existing encryption key to the
// parameters specified in modeenv.
// It is *very intentional* that resealing takes the modeenv and only
// the modeenv as input. modeenv content is well defined and updated
// atomically.  In particular we want to avoid resealing against
// transient/in-memory information with the risk that successive
// reseals during in-progress operations produce diverging outcomes.
func resealKeyToModeenvImpl(rootdir string, modeenv *Modeenv, expectReseal bool, unlocker Unlocker) error {
	if !isModeeenvLocked() {
		return fmt.Errorf("internal error: cannot reseal without the modeenv lock")
	}

	method, err := device.SealedKeysMethod(rootdir)
	if err == device.ErrNoSealedKeys {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}

	return resealKeyToModeenvForMethod(unlocker, method, rootdir, modeenv, expectReseal)
}

type ResealKeyForBootChainsParams struct {
	// RunModeBootChains are the boot chains run for key role
	RunModeBootChains []BootChain
	// RecoveryBootChainsForRunKey are the extra boot chains for
	// run+recover key role
	RecoveryBootChainsForRunKey []BootChain
	// RecoveryBootChains are the boot chains for recover key role
	RecoveryBootChains []BootChain
	// RoleToBlName maps bootloader role to the name of its bootloader
	RoleToBlName map[bootloader.Role]string
}

// WithBootChains calls the provided function passing the boot chains which may
// be observed when booting as an input. The boot can be used as an input for
// resealing of disk encryption keys. The modeenv is locked internally, hence
// resealing is safe to perform
func WithBootChains(f func(bc *ResealKeyForBootChainsParams) error) error {
	modeenvLock()
	defer modeenvUnlock()

	m, err := loadModeenv()
	if err != nil {
		return err
	}

	bc, err := bootChains(m)
	if err != nil {
		return err
	}

	return f(bc)
}

// bootChains constructs the boot chains which may be observed when booting the
// device such that they can be used as an input for resealing of encryption
// keys.
func bootChains(modeenv *Modeenv) (*ResealKeyForBootChainsParams, error) {
	params := &ResealKeyForBootChainsParams{}

	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return nil, fmt.Errorf("internal error: sealed keys but not a trusted assets bootloader")
	}
	// derive the allowed modes for each system mentioned in the modeenv
	modes := modesForSystems(modeenv)

	// the recovery boot chains for the run key are generated for all
	// recovery systems, including those that are being tried; since this is
	// a run key, the boot chains are generated for both models to
	// accommodate the dynamics of a remodel
	includeTryModel := true
	params.RecoveryBootChainsForRunKey, err = recoveryBootChainsForSystems(modeenv.CurrentRecoverySystems, modes, tbl,
		modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return nil, fmt.Errorf("cannot compose recovery boot chains for run key: %v", err)
	}

	// the boot chains for recovery keys include only those system that were
	// tested and are known to be good
	testedRecoverySystems := modeenv.GoodRecoverySystems
	if len(testedRecoverySystems) == 0 && len(modeenv.CurrentRecoverySystems) > 0 {
		// compatibility for systems where good recovery systems list
		// has not been populated yet
		testedRecoverySystems = modeenv.CurrentRecoverySystems[:1]
		logger.Noticef("no good recovery systems for reseal, fallback to known current system %v",
			testedRecoverySystems[0])
	}
	// use the current model as the recovery keys are not expected to be
	// used during a remodel
	includeTryModel = false
	params.RecoveryBootChains, err = recoveryBootChainsForSystems(testedRecoverySystems, modes, tbl, modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return nil, fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot find the bootloader: %v", err)
	}
	cmdlines, err := kernelCommandLinesForResealWithFallback(modeenv)
	if err != nil {
		return nil, err
	}
	params.RunModeBootChains, err = runModeBootChains(tbl, bl, modeenv, cmdlines, "")
	if err != nil {
		return nil, fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	params.RoleToBlName = map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	return params, nil
}

func resealKeyToModeenvForMethod(unlocker Unlocker, method device.SealingMethod, rootdir string, modeenv *Modeenv, expectReseal bool) error {
	// this is just optimization. If the backend does not need it, we should not calculate it.
	requiresBootChains := true
	switch method {
	case device.SealingMethodFDESetupHook:
		requiresBootChains = false
	}

	params := &ResealKeyForBootChainsParams{}

	if requiresBootChains {
		bc, err := bootChains(modeenv)
		if err != nil {
			return err
		}
		params = bc
	}

	return ResealKeyForBootChains(unlocker, method, rootdir, params, expectReseal)
}

func resealKeyForBootChainsImpl(unlocker Unlocker, method device.SealingMethod, rootdir string, params *ResealKeyForBootChainsParams, expectReseal bool) error {
	return fmt.Errorf("FDE manager was not started")
}

var ResealKeyForBootChains = resealKeyForBootChainsImpl

// modesForSystems returns a map for recovery modes for recovery systems
// mentioned in the modeenv. The returned map contains both tested and candidate
// recovery systems
func modesForSystems(modeenv *Modeenv) map[string][]string {
	if len(modeenv.GoodRecoverySystems) == 0 && len(modeenv.CurrentRecoverySystems) == 0 {
		return nil
	}

	systemToModes := map[string][]string{}

	// first go through tested recovery systems
	modesForTestedSystem := []string{ModeRecover, ModeFactoryReset}
	// tried systems can only boot to recovery mode
	modesForCandidateSystem := []string{ModeRecover}

	// go through current recovery systems which can contain both tried
	// systems and candidate ones
	for _, sys := range modeenv.CurrentRecoverySystems {
		systemToModes[sys] = modesForCandidateSystem
	}
	// go through recovery systems that were tested and update their modes
	for _, sys := range modeenv.GoodRecoverySystems {
		systemToModes[sys] = modesForTestedSystem
	}
	return systemToModes
}

func recoveryBootChainsForSystems(systems []string, modesForSystems map[string][]string, trbl bootloader.TrustedAssetsBootloader, modeenv *Modeenv, includeTryModel bool, seedDir string) (chains []BootChain, err error) {
	if trbl == nil {
		return recoveryBootChainsForSystemsWithoutTrustedAssets(systems, modesForSystems, modeenv, includeTryModel, seedDir)
	}

	return recoveryBootChainsForSystemsWithTrustedAssets(systems, modesForSystems, trbl, modeenv, includeTryModel, seedDir)
}

func recoveryBootChainsForSystemsWithoutTrustedAssets(systems []string, modesForSystems map[string][]string, modeenv *Modeenv, includeTryModel bool, seedDir string) (chains []BootChain, err error) {
	chainsForModel := func(model secboot.ModelForSealing) error {
		for _, system := range systems {
			var cmdlines []string
			modes, ok := modesForSystems[system]
			if !ok {
				return fmt.Errorf("internal error: no modes for system %q", system)
			}

			for _, mode := range modes {
				// FIXME: we do not really know the
				// command line. But we do know the
				// mode and system we should give that
				// to the fde manager.
				switch mode {
				case ModeRun:
					cmdlines = append(cmdlines, "snapd_recovery_mode=run")
				case ModeRecover:
					cmdlines = append(cmdlines, fmt.Sprintf("snapd_recovery_system=%v snapd_recovery_mode=recover", system))
				case ModeFactoryReset:
					cmdlines = append(cmdlines, fmt.Sprintf("snapd_recovery_system=%v snapd_recovery_mode=factory-reset", system))
				}
			}

			chains = append(chains, BootChain{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Classic:        model.Classic(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
				KernelCmdlines: cmdlines,
			})
		}
		return nil
	}

	if err := chainsForModel(modeenv.ModelForSealing()); err != nil {
		return nil, err
	}

	if modeenv.TryModel != "" && includeTryModel {
		if err := chainsForModel(modeenv.TryModelForSealing()); err != nil {
			return nil, err
		}
	}

	return chains, nil
}

func recoveryBootChainsForSystemsWithTrustedAssets(systems []string, modesForSystems map[string][]string, trbl bootloader.TrustedAssetsBootloader, modeenv *Modeenv, includeTryModel bool, seedDir string) (chains []BootChain, err error) {
	trustedAssets, err := trbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	chainsForModel := func(model secboot.ModelForSealing) error {
		modelID := modelUniqueID(model)
		for _, system := range systems {
			// get kernel and gadget information from seed
			perf := timings.New(nil)
			seedSystemModel, snaps, err := seedReadSystemEssential(seedDir, system, []snap.Type{snap.TypeKernel, snap.TypeGadget}, perf)
			if err != nil {
				return fmt.Errorf("cannot read system %q seed: %v", system, err)
			}
			if len(snaps) != 2 {
				return fmt.Errorf("cannot obtain recovery system snaps")
			}
			seedModelID := modelUniqueID(seedSystemModel)
			// TODO: the generated unique ID contains the model's
			// sign key ID, consider relaxing this to ignore the key
			// ID when matching models, OTOH we would need to
			// properly take into account key expiration and
			// revocation
			if seedModelID != modelID {
				// could be an incompatible recovery system that
				// is still currently tracked in modeenv
				continue
			}
			seedKernel, seedGadget := snaps[0], snaps[1]
			if snaps[0].EssentialType == snap.TypeGadget {
				seedKernel, seedGadget = seedGadget, seedKernel
			}

			var cmdlines []string
			modes, ok := modesForSystems[system]
			if !ok {
				return fmt.Errorf("internal error: no modes for system %q", system)
			}
			for _, mode := range modes {
				// get the command line for this mode
				cmdline, err := composeCommandLine(currentEdition, mode, system, seedGadget.Path, model)
				if err != nil {
					return fmt.Errorf("cannot obtain kernel command line for mode %q: %v", mode, err)
				}
				cmdlines = append(cmdlines, cmdline)
			}

			var kernelRev string
			if seedKernel.SideInfo.Revision.Store() {
				kernelRev = seedKernel.SideInfo.Revision.String()
			}

			recoveryBootChains, err := trbl.RecoveryBootChains(seedKernel.Path)
			if err != nil {
				return err
			}

			foundChain := false

			// get asset chains
			for _, recoveryBootChain := range recoveryBootChains {
				assetChain, kbf, err := buildBootAssets(recoveryBootChain, modeenv, trustedAssets)
				if err != nil {
					return err
				}
				if assetChain == nil {
					// This chain is not used as
					// it is not in the modeenv,
					// we expect another chain to
					// work.
					continue
				}

				chains = append(chains, BootChain{
					BrandID: model.BrandID(),
					Model:   model.Model(),
					// TODO: test this
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),
					AssetChain:     assetChain,
					Kernel:         seedKernel.SnapName(),
					KernelRevision: kernelRev,
					KernelCmdlines: cmdlines,
					KernelBootFile: kbf,
				})

				foundChain = true
			}

			if !foundChain {
				return fmt.Errorf("could not find any valid chain for this model")
			}
		}
		return nil
	}

	if err := chainsForModel(modeenv.ModelForSealing()); err != nil {
		return nil, err
	}

	if modeenv.TryModel != "" && includeTryModel {
		if err := chainsForModel(modeenv.TryModelForSealing()); err != nil {
			return nil, err
		}
	}

	return chains, nil
}

func runModeBootChains(rbl bootloader.TrustedAssetsBootloader, bl bootloader.Bootloader, modeenv *Modeenv, cmdlines []string, runSnapsDir string) ([]BootChain, error) {
	if rbl == nil {
		return runModeBootChainsWithoutTrustedAssets(modeenv, runSnapsDir)
	} else {
		return runModeBootChainsWithTrustedAssets(rbl, bl, modeenv, cmdlines, runSnapsDir)
	}
}

func runModeBootChainsWithoutTrustedAssets(modeenv *Modeenv, runSnapsDir string) ([]BootChain, error) {
	var chains []BootChain

	chainsForModel := func(model secboot.ModelForSealing) {
		chains = append(chains, BootChain{
			BrandID:        model.BrandID(),
			Model:          model.Model(),
			Classic:        model.Classic(),
			Grade:          model.Grade(),
			ModelSignKeyID: model.SignKeyID(),
			// FIXME: the fde manager will need the run mode. Not the kernel command line.
			KernelCmdlines: []string{"snapd_recovery_mode=run"},
		})
	}
	chainsForModel(modeenv.ModelForSealing())

	if modeenv.TryModel != "" {
		chainsForModel(modeenv.TryModelForSealing())
	}

	return chains, nil
}

func runModeBootChainsWithTrustedAssets(tbl bootloader.TrustedAssetsBootloader, bl bootloader.Bootloader, modeenv *Modeenv, cmdlines []string, runSnapsDir string) ([]BootChain, error) {
	chains := make([]BootChain, 0, len(modeenv.CurrentKernels))

	trustedAssets, err := tbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	chainsForModel := func(model secboot.ModelForSealing) error {
		for _, k := range modeenv.CurrentKernels {
			info, err := snap.ParsePlaceInfoFromSnapFileName(k)
			if err != nil {
				return err
			}
			var kernelPath string
			if runSnapsDir == "" {
				kernelPath = info.MountFile()
			} else {
				kernelPath = filepath.Join(runSnapsDir, info.Filename())
			}
			runModeBootChains, err := tbl.BootChains(bl, kernelPath)
			if err != nil {
				return err
			}

			foundChain := false

			for _, runModeBootChain := range runModeBootChains {
				// get asset chains
				assetChain, kbf, err := buildBootAssets(runModeBootChain, modeenv, trustedAssets)
				if err != nil {
					return err
				}
				if assetChain == nil {
					// This chain is not used as
					// it is not in the modeenv,
					// we expect another chain to
					// work.
					continue
				}
				var kernelRev string
				if info.SnapRevision().Store() {
					kernelRev = info.SnapRevision().String()
				}
				chains = append(chains, BootChain{
					BrandID: model.BrandID(),
					Model:   model.Model(),
					// TODO: test this
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),
					AssetChain:     assetChain,
					Kernel:         info.SnapName(),
					KernelRevision: kernelRev,
					KernelCmdlines: cmdlines,
					KernelBootFile: kbf,
				})
				foundChain = true
			}

			if !foundChain {
				return fmt.Errorf("could not find any valid chain for this model")
			}
		}
		return nil
	}
	if err := chainsForModel(modeenv.ModelForSealing()); err != nil {
		return nil, err
	}

	if modeenv.TryModel != "" {
		if err := chainsForModel(modeenv.TryModelForSealing()); err != nil {
			return nil, err
		}
	}
	return chains, nil
}

// buildBootAssets takes the BootFiles of a bootloader boot chain and
// produces corresponding bootAssets with the matching current asset
// hashes from modeenv plus it returns separately the last BootFile
// which is for the kernel.
func buildBootAssets(bootFiles []bootloader.BootFile, modeenv *Modeenv, trustedAssets map[string]string) (assets []BootAsset, kernel bootloader.BootFile, err error) {
	if len(bootFiles) == 0 {
		// useful in testing, when mocking is insufficient
		return nil, bootloader.BootFile{}, fmt.Errorf("internal error: cannot build boot assets without boot files")
	}
	assets = make([]BootAsset, len(bootFiles)-1)

	// the last element is the kernel which is not a boot asset
	for i, bf := range bootFiles[:len(bootFiles)-1] {
		path := bf.Path
		name, ok := trustedAssets[path]
		if !ok {
			return nil, kernel, fmt.Errorf("internal error: asset '%s' is not considered a trusted asset for the bootloader", path)
		}
		var hashes []string
		if bf.Role == bootloader.RoleRecovery {
			hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
		} else {
			hashes, ok = modeenv.CurrentTrustedBootAssets[name]
		}
		if !ok {
			// We have not found an asset for this
			// chain. There are chains expected to not
			// exist. So we return without error.
			// recoveryBootChainsForSystems and
			// runModeBootChains will fail if no chain is
			// found
			return nil, kernel, nil
		}
		assets[i] = BootAsset{
			Role:   bf.Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, bootFiles[len(bootFiles)-1], nil
}

func SealKeyModelParams(pbc PredictableBootChains, roleToBlName map[bootloader.Role]string) ([]*secboot.SealKeyModelParams, error) {
	// seal parameters keyed by unique model ID
	modelToParams := map[string]*secboot.SealKeyModelParams{}
	modelParams := make([]*secboot.SealKeyModelParams, 0, len(pbc))

	for _, bc := range pbc {
		modelForSealing := bc.ModelForSealing()
		modelID := modelUniqueID(modelForSealing)
		const expectNew = false
		loadChains, err := bootAssetsToLoadChains(bc.AssetChain, bc.KernelBootFile, roleToBlName, expectNew)
		if err != nil {
			return nil, fmt.Errorf("cannot build load chains with current boot assets: %s", err)
		}

		// group parameters by model, reuse an existing SealKeyModelParams
		// if the model is the same.
		if params, ok := modelToParams[modelID]; ok {
			params.KernelCmdlines = strutil.SortedListsUniqueMerge(params.KernelCmdlines, bc.KernelCmdlines)
			params.EFILoadChains = append(params.EFILoadChains, loadChains...)
		} else {
			param := &secboot.SealKeyModelParams{
				Model:          modelForSealing,
				KernelCmdlines: bc.KernelCmdlines,
				EFILoadChains:  loadChains,
			}
			modelParams = append(modelParams, param)
			modelToParams[modelID] = param
		}
	}

	return modelParams, nil
}

// IsResealNeeded returns true when the predictable boot chains provided as
// input do not match the cached boot chains on disk under rootdir.
// It also returns the next value for the reseal count that is saved
// together with the boot chains.
// A hint expectReseal can be provided, it is used when the matching
// is ambigous because the boot chains contain unrevisioned kernels.
func IsResealNeeded(pbc PredictableBootChains, bootChainsFile string, expectReseal bool) (ok bool, nextCount int, err error) {
	previousPbc, c, err := ReadBootChains(bootChainsFile)
	if err != nil {
		return false, 0, err
	}

	switch predictableBootChainsEqualForReseal(pbc, previousPbc) {
	case bootChainEquivalent:
		return false, c + 1, nil
	case bootChainUnrevisioned:
		return expectReseal, c + 1, nil
	case bootChainDifferent:
	}
	return true, c + 1, nil
}

func postFactoryResetCleanupSecboot() error {
	// we are inspecting a key which was generated during factory reset, in
	// the simplest case the sealed key generated previously used the main
	// handles, while the current key uses alt handles, hence we need to
	// release the main handles corresponding to the old key
	handles := []uint32{secboot.RunObjectPCRPolicyCounterHandle, secboot.FallbackObjectPCRPolicyCounterHandle}
	usesAlt, err := UsesAltPCRHandles()
	if err != nil {
		return fmt.Errorf("cannot inspect fallback key: %v", err)
	}
	if !usesAlt {
		// current fallback key using the main handles, which is
		// possible of there were subsequent factory reset steps,
		// release the alt handles associated with the old key
		handles = []uint32{secboot.AltRunObjectPCRPolicyCounterHandle, secboot.AltFallbackObjectPCRPolicyCounterHandle}
	}
	return secbootReleasePCRResourceHandles(handles...)
}

func postFactoryResetCleanup() error {
	hasHook, err := HasFDESetupHook(nil)
	if err != nil {
		return fmt.Errorf("cannot check for fde-setup hook %v", err)
	}

	saveFallbackKeyFactory := device.FactoryResetFallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	saveFallbackKey := device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	if err := os.Rename(saveFallbackKeyFactory, saveFallbackKey); err != nil {
		// it is possible that the key file was already renamed if we
		// came back here after an unexpected reboot
		if !os.IsNotExist(err) {
			return fmt.Errorf("cannot rotate fallback key: %v", err)
		}
	}

	if hasHook {
		// TODO: do we need to invoke FDE hook?
		return nil
	}

	if err := postFactoryResetCleanupSecboot(); err != nil {
		return fmt.Errorf("cannot cleanup secboot state: %v", err)
	}

	return nil
}

// resealExpectedByModeenvChange returns true if resealing is expected
// due to modeenv changes, false otherwise. Reseal might not be needed
// if the only change in modeenv is the gadget (if the boot assets
// change that is detected in resealKeyToModeenv() and reseal will
// happen anyway)
func resealExpectedByModeenvChange(m1, m2 *Modeenv) bool {
	auxModeenv := *m2
	auxModeenv.Gadget = m1.Gadget
	return !auxModeenv.deepEqual(m1)
}

func MockSecbootPCRHandleOfSealedKey(f func(p string) (uint32, error)) (restore func()) {
	osutil.MustBeTestBinary("mock PCRHandleOfSealedKey only to be used in tests")
	restore = testutil.Backup(&secbootPCRHandleOfSealedKey)
	secbootPCRHandleOfSealedKey = f
	return restore
}

func MockSecbootReleasePCRResourceHandles(f func(handles ...uint32) error) (restore func()) {
	osutil.MustBeTestBinary("mock ReleasePCRResourceHandles only to be used in tests")
	restore = testutil.Backup(&secbootReleasePCRResourceHandles)
	secbootReleasePCRResourceHandles = f
	return restore
}
