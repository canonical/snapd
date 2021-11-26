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

package boot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	secbootSealKeys                 = secboot.SealKeys
	secbootSealKeysWithFDESetupHook = secboot.SealKeysWithFDESetupHook
	secbootResealKeys               = secboot.ResealKeys

	seedReadSystemEssential = seed.ReadSystemEssential
)

// Hook functions setup by devicestate to support device-specific full
// disk encryption implementations. The state must be locked when these
// functions are called.
var (
	HasFDESetupHook = func() (bool, error) {
		return false, nil
	}
	RunFDESetupHook fde.RunSetupHookFunc = func(req *fde.SetupRequest) ([]byte, error) {
		return nil, fmt.Errorf("internal error: RunFDESetupHook not set yet")
	}
)

type sealingMethod string

const (
	sealingMethodLegacyTPM    = sealingMethod("")
	sealingMethodTPM          = sealingMethod("tpm")
	sealingMethodFDESetupHook = sealingMethod("fde-setup-hook")
)

// MockSecbootResealKeys is only useful in testing. Note that this is a very low
// level call and may need significant environment setup.
func MockSecbootResealKeys(f func(params *secboot.ResealKeysParams) error) (restore func()) {
	osutil.MustBeTestBinary("secbootResealKeys only can be mocked in tests")
	old := secbootResealKeys
	secbootResealKeys = f
	return func() {
		secbootResealKeys = old
	}
}

// MockResealKeyToModeenv is only useful in testing.
func MockResealKeyToModeenv(f func(rootdir string, modeenv *Modeenv, expectReseal bool) error) (restore func()) {
	osutil.MustBeTestBinary("resealKeyToModeenv only can be mocked in tests")
	old := resealKeyToModeenv
	resealKeyToModeenv = f
	return func() {
		resealKeyToModeenv = old
	}
}

func bootChainsFileUnder(rootdir string) string {
	return filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains")
}

func recoveryBootChainsFileUnder(rootdir string) string {
	return filepath.Join(dirs.SnapFDEDirUnder(rootdir), "recovery-boot-chains")
}

// sealKeyToModeenv seals the supplied keys to the parameters specified
// in modeenv.
// It assumes to be invoked in install mode.
func sealKeyToModeenv(key, saveKey secboot.EncryptionKey, model *asserts.Model, modeenv *Modeenv) error {
	// make sure relevant locations exist
	for _, p := range []string{
		InitramfsSeedEncryptionKeyDir,
		InitramfsBootEncryptionKeyDir,
		InstallHostFDEDataDir,
		InstallHostFDESaveDir,
	} {
		// XXX: should that be 0700 ?
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}

	hasHook, err := HasFDESetupHook()
	if err != nil {
		return fmt.Errorf("cannot check for fde-setup hook %v", err)
	}
	if hasHook {
		return sealKeyToModeenvUsingFDESetupHook(key, saveKey, modeenv)
	}

	return sealKeyToModeenvUsingSecboot(key, saveKey, modeenv)
}

func runKeySealRequests(key secboot.EncryptionKey) []secboot.SealKeyRequest {
	return []secboot.SealKeyRequest{
		{
			Key:     key,
			KeyName: "ubuntu-data",
			KeyFile: filepath.Join(InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
		},
	}
}

func fallbackKeySealRequests(key, saveKey secboot.EncryptionKey) []secboot.SealKeyRequest {
	return []secboot.SealKeyRequest{
		{
			Key:     key,
			KeyName: "ubuntu-data",
			KeyFile: filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		},
		{
			Key:     saveKey,
			KeyName: "ubuntu-save",
			KeyFile: filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		},
	}
}

func sealKeyToModeenvUsingFDESetupHook(key, saveKey secboot.EncryptionKey, modeenv *Modeenv) error {
	// XXX: Move the auxKey creation to a more generic place, see
	// PR#10123 for a possible way of doing this. However given
	// that the equivalent key for the TPM case is also created in
	// sealKeyToModeenvUsingTPM more symetric to create the auxKey
	// here and when we also move TPM to use the auxKey to move
	// the creation of it.
	auxKey, err := secboot.NewAuxKey()
	if err != nil {
		return fmt.Errorf("cannot create aux key: %v", err)
	}
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      modeenv.ModelForSealing(),
		AuxKey:     auxKey,
		AuxKeyFile: filepath.Join(InstallHostFDESaveDir, "aux-key"),
	}
	skrs := append(runKeySealRequests(key), fallbackKeySealRequests(key, saveKey)...)
	if err := secbootSealKeysWithFDESetupHook(RunFDESetupHook, skrs, &params); err != nil {
		return err
	}

	if err := stampSealedKeys(InstallHostWritableDir, "fde-setup-hook"); err != nil {
		return err
	}

	return nil
}

func sealKeyToModeenvUsingSecboot(key, saveKey secboot.EncryptionKey, modeenv *Modeenv) error {
	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return fmt.Errorf("internal error: cannot seal keys without a trusted assets bootloader")
	}

	includeTryModel := false
	recoveryBootChains, err := recoveryBootChainsForSystems([]string{modeenv.RecoverySystem}, tbl, modeenv, includeTryModel)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}

	// kernel command lines are filled during install
	cmdlines := modeenv.CurrentKernelCommandLines
	runModeBootChains, err := runModeBootChains(rbl, bl, modeenv, cmdlines)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChains...))

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	// the boot chains we seal the fallback object to
	rpbc := toPredictableBootChains(recoveryBootChains)

	// gets written to a file by sealRunObjectKeys()
	authKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("cannot generate key for signing dynamic authorization policies: %v", err)
	}

	if err := sealRunObjectKeys(key, pbc, authKey, roleToBlName); err != nil {
		return err
	}

	if err := sealFallbackObjectKeys(key, saveKey, rpbc, authKey, roleToBlName); err != nil {
		return err
	}

	if err := stampSealedKeys(InstallHostWritableDir, sealingMethodTPM); err != nil {
		return err
	}

	installBootChainsPath := bootChainsFileUnder(InstallHostWritableDir)
	if err := writeBootChains(pbc, installBootChainsPath, 0); err != nil {
		return err
	}

	installRecoveryBootChainsPath := recoveryBootChainsFileUnder(InstallHostWritableDir)
	if err := writeBootChains(rpbc, installRecoveryBootChainsPath, 0); err != nil {
		return err
	}

	return nil
}

func sealRunObjectKeys(key secboot.EncryptionKey, pbc predictableBootChains, authKey *ecdsa.PrivateKey, roleToBlName map[bootloader.Role]string) error {
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key sealing: %v", err)
	}

	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:            modelParams,
		TPMPolicyAuthKey:       authKey,
		TPMPolicyAuthKeyFile:   filepath.Join(InstallHostFDESaveDir, "tpm-policy-auth-key"),
		TPMLockoutAuthFile:     filepath.Join(InstallHostFDESaveDir, "tpm-lockout-auth"),
		TPMProvision:           true,
		PCRPolicyCounterHandle: secboot.RunObjectPCRPolicyCounterHandle,
	}
	// The run object contains only the ubuntu-data key; the ubuntu-save key
	// is then stored inside the encrypted data partition, so that the normal run
	// path only unseals one object because unsealing is expensive.
	// Furthermore, the run object key is stored on ubuntu-boot so that we do not
	// need to continually write/read keys from ubuntu-seed.
	if err := secbootSealKeys(runKeySealRequests(key), sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the encryption keys: %v", err)
	}

	return nil
}

func sealFallbackObjectKeys(key, saveKey secboot.EncryptionKey, pbc predictableBootChains, authKey *ecdsa.PrivateKey, roleToBlName map[bootloader.Role]string) error {
	// also seal the keys to the recovery bootchains as a fallback
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key sealing: %v", err)
	}
	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:            modelParams,
		TPMPolicyAuthKey:       authKey,
		PCRPolicyCounterHandle: secboot.FallbackObjectPCRPolicyCounterHandle,
	}
	// The fallback object contains the ubuntu-data and ubuntu-save keys. The
	// key files are stored on ubuntu-seed, separate from ubuntu-data so they
	// can be used if ubuntu-data and ubuntu-boot are corrupted or unavailable.
	if err := secbootSealKeys(fallbackKeySealRequests(key, saveKey), sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the fallback encryption keys: %v", err)
	}

	return nil
}

func stampSealedKeys(rootdir string, content sealingMethod) error {
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return fmt.Errorf("cannot create device fde state directory: %v", err)
	}

	if err := osutil.AtomicWriteFile(stamp, []byte(content), 0644, 0); err != nil {
		return fmt.Errorf("cannot create fde sealed keys stamp file: %v", err)
	}
	return nil
}

var errNoSealedKeys = errors.New("no sealed keys")

// sealedKeysMethod return whether any keys were sealed at all
func sealedKeysMethod(rootdir string) (sm sealingMethod, err error) {
	// TODO:UC20: consider more than the marker for cases where we reseal
	// outside of run mode
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	content, err := ioutil.ReadFile(stamp)
	if os.IsNotExist(err) {
		return sm, errNoSealedKeys
	}
	return sealingMethod(content), err
}

var resealKeyToModeenv = resealKeyToModeenvImpl

// resealKeyToModeenv reseals the existing encryption key to the
// parameters specified in modeenv.
// It is *very intentional* that resealing takes the modeenv and only
// the modeenv as input. modeenv content is well defined and updated
// atomically.  In particular we want to avoid resealing against
// transient/in-memory information with the risk that successive
// reseals during in-progress operations produce diverging outcomes.
func resealKeyToModeenvImpl(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	method, err := sealedKeysMethod(rootdir)
	if err == errNoSealedKeys {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}
	switch method {
	case sealingMethodFDESetupHook:
		return resealKeyToModeenvUsingFDESetupHook(rootdir, modeenv, expectReseal)
	case sealingMethodTPM, sealingMethodLegacyTPM:
		return resealKeyToModeenvSecboot(rootdir, modeenv, expectReseal)
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}
}

var resealKeyToModeenvUsingFDESetupHook = resealKeyToModeenvUsingFDESetupHookImpl

func resealKeyToModeenvUsingFDESetupHookImpl(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	// TODO: we need to implement reseal at least in terms of
	//       rebinding the keys to models on remodeling

	// TODO: If we have situations that do TPM-like full sealing then:
	//       Implement reseal using the fde-setup hook. This will
	//       require a helper like "FDEShouldResealUsingSetupHook"
	//       that will be set by devicestate and returns (bool,
	//       error).  It needs to return "false" during seeding
	//       because then there is no kernel available yet.  It
	//       can though return true as soon as there's an active
	//       kernel if seeded is false
	//
	//       It will also need to run HasFDESetupHook internally
	//       and return an error if the hook goes missing
	//       (e.g. because a kernel refresh losses the hook by
	//       accident). It could also run features directly and
	//       check for "reseal" in features.
	return nil
}

// TODO:UC20: allow more than one model to accommodate the remodel scenario
func resealKeyToModeenvSecboot(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return fmt.Errorf("internal error: sealed keys but not a trusted assets bootloader")
	}

	// the recovery boot chains for the run key are generated for all
	// recovery systems, including those that are being tried; since this is
	// a run key, the boot chains are generated for both models to
	// accommodate the dynamics of a remodel
	includeTryModel := true
	recoveryBootChainsForRunKey, err := recoveryBootChainsForSystems(modeenv.CurrentRecoverySystems, tbl,
		modeenv, includeTryModel)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains for run key: %v", err)
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
	recoveryBootChains, err := recoveryBootChainsForSystems(testedRecoverySystems, tbl, modeenv, includeTryModel)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}
	cmdlines, err := kernelCommandLinesForResealWithFallback(modeenv)
	if err != nil {
		return err
	}
	runModeBootChains, err := runModeBootChains(rbl, bl, modeenv, cmdlines)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}
	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
	authKeyFile := filepath.Join(saveFDEDir, "tpm-policy-auth-key")

	// reseal the run object
	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChainsForRunKey...))

	needed, nextCount, err := isResealNeeded(pbc, bootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		if err := resealRunObjectKeys(pbc, authKeyFile, roleToBlName); err != nil {
			return err
		}
		logger.Debugf("resealing (%d) succeeded", nextCount)

		bootChainsPath := bootChainsFileUnder(rootdir)
		if err := writeBootChains(pbc, bootChainsPath, nextCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("reseal not necessary")
	}

	// reseal the fallback object
	rpbc := toPredictableBootChains(recoveryBootChains)

	var nextFallbackCount int
	needed, nextFallbackCount, err = isResealNeeded(rpbc, recoveryBootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		rpbcJSON, _ := json.Marshal(rpbc)
		logger.Debugf("resealing (%d) to recovery boot chains: %s", nextFallbackCount, rpbcJSON)

		if err := resealFallbackObjectKeys(rpbc, authKeyFile, roleToBlName); err != nil {
			return err
		}
		logger.Debugf("fallback resealing (%d) succeeded", nextFallbackCount)

		recoveryBootChainsPath := recoveryBootChainsFileUnder(rootdir)
		if err := writeBootChains(rpbc, recoveryBootChainsPath, nextFallbackCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("fallback reseal not necessary")
	}

	return nil
}

func resealRunObjectKeys(pbc predictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key resealing: %v", err)
	}

	// list all the key files to reseal
	keyFiles := []string{
		filepath.Join(InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
	}

	resealKeyParams := &secboot.ResealKeysParams{
		ModelParams:          modelParams,
		KeyFiles:             keyFiles,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the encryption key: %v", err)
	}

	return nil
}

func resealFallbackObjectKeys(pbc predictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key resealing: %v", err)
	}

	// list all the key files to reseal
	keyFiles := []string{
		filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
	}

	resealKeyParams := &secboot.ResealKeysParams{
		ModelParams:          modelParams,
		KeyFiles:             keyFiles,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the fallback encryption keys: %v", err)
	}

	return nil
}

// TODO:UC20: this needs to take more than one model to accommodate the remodel
// scenario
func recoveryBootChainsForSystems(systems []string, trbl bootloader.TrustedAssetsBootloader, modeenv *Modeenv, includeTryModel bool) (chains []bootChain, err error) {

	chainsForModel := func(model secboot.ModelForSealing) error {
		modelID := modelUniqueID(model)
		for _, system := range systems {
			// get kernel and gadget information from seed
			perf := timings.New(nil)
			seedSystemModel, snaps, err := seedReadSystemEssential(dirs.SnapSeedDir, system, []snap.Type{snap.TypeKernel, snap.TypeGadget}, perf)
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

			// get the command line
			cmdline, err := composeCommandLine(currentEdition, ModeRecover, system, seedGadget.Path)
			if err != nil {
				return fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
			}

			var kernelRev string
			if seedKernel.SideInfo.Revision.Store() {
				kernelRev = seedKernel.SideInfo.Revision.String()
			}

			recoveryBootChain, err := trbl.RecoveryBootChain(seedKernel.Path)
			if err != nil {
				return err
			}

			// get asset chains
			assetChain, kbf, err := buildBootAssets(recoveryBootChain, modeenv)
			if err != nil {
				return err
			}

			chains = append(chains, bootChain{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
				AssetChain:     assetChain,
				Kernel:         seedKernel.SnapName(),
				KernelRevision: kernelRev,
				KernelCmdlines: []string{cmdline},
				kernelBootFile: kbf,
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

func runModeBootChains(rbl, bl bootloader.Bootloader, modeenv *Modeenv, cmdlines []string) ([]bootChain, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}
	chains := make([]bootChain, 0, len(modeenv.CurrentKernels))

	chainsForModel := func(model secboot.ModelForSealing) error {
		for _, k := range modeenv.CurrentKernels {
			info, err := snap.ParsePlaceInfoFromSnapFileName(k)
			if err != nil {
				return err
			}
			runModeBootChain, err := tbl.BootChain(bl, info.MountFile())
			if err != nil {
				return err
			}

			// get asset chains
			assetChain, kbf, err := buildBootAssets(runModeBootChain, modeenv)
			if err != nil {
				return err
			}
			var kernelRev string
			if info.SnapRevision().Store() {
				kernelRev = info.SnapRevision().String()
			}
			chains = append(chains, bootChain{
				BrandID:        model.BrandID(),
				Model:          model.Model(),
				Grade:          model.Grade(),
				ModelSignKeyID: model.SignKeyID(),
				AssetChain:     assetChain,
				Kernel:         info.SnapName(),
				KernelRevision: kernelRev,
				KernelCmdlines: cmdlines,
				kernelBootFile: kbf,
			})
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
func buildBootAssets(bootFiles []bootloader.BootFile, modeenv *Modeenv) (assets []bootAsset, kernel bootloader.BootFile, err error) {
	if len(bootFiles) == 0 {
		// useful in testing, when mocking is insufficient
		return nil, bootloader.BootFile{}, fmt.Errorf("internal error: cannot build boot assets without boot files")
	}
	assets = make([]bootAsset, len(bootFiles)-1)

	// the last element is the kernel which is not a boot asset
	for i, bf := range bootFiles[:len(bootFiles)-1] {
		name := filepath.Base(bf.Path)
		var hashes []string
		var ok bool
		if bf.Role == bootloader.RoleRecovery {
			hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
		} else {
			hashes, ok = modeenv.CurrentTrustedBootAssets[name]
		}
		if !ok {
			return nil, kernel, fmt.Errorf("cannot find expected boot asset %s in modeenv", name)
		}
		assets[i] = bootAsset{
			Role:   bf.Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, bootFiles[len(bootFiles)-1], nil
}

func sealKeyModelParams(pbc predictableBootChains, roleToBlName map[bootloader.Role]string) ([]*secboot.SealKeyModelParams, error) {
	// seal parameters keyed by unique model ID
	modelToParams := map[string]*secboot.SealKeyModelParams{}
	modelParams := make([]*secboot.SealKeyModelParams, 0, len(pbc))

	for _, bc := range pbc {
		modelForSealing := bc.modelForSealing()
		modelID := modelUniqueID(modelForSealing)
		loadChains, err := bootAssetsToLoadChains(bc.AssetChain, bc.kernelBootFile, roleToBlName)
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

// isResealNeeded returns true when the predictable boot chains provided as
// input do not match the cached boot chains on disk under rootdir.
// It also returns the next value for the reseal count that is saved
// together with the boot chains.
// A hint expectReseal can be provided, it is used when the matching
// is ambigous because the boot chains contain unrevisioned kernels.
func isResealNeeded(pbc predictableBootChains, bootChainsFile string, expectReseal bool) (ok bool, nextCount int, err error) {
	previousPbc, c, err := readBootChains(bootChainsFile)
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
