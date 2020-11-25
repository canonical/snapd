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
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	secbootSealKeys   = secboot.SealKeys
	secbootResealKeys = secboot.ResealKeys

	seedReadSystemEssential = seed.ReadSystemEssential
)

// Hook functions setup by devicestate to support device-specific full
// disk encryption implementations.
var (
	HasFDESetupHook = func() (bool, error) {
		return false, nil
	}
	RunFDESetupHook = func(op string, params *FdeSetupHookParams) ([]byte, error) {
		return nil, fmt.Errorf("internal error: RunFDESetupHook not set yet")
	}
)

// FdeSetupHookParams contains the inputs for the fde-setup hook
type FdeSetupHookParams struct {
	Key     *secboot.EncryptionKey
	KeyName string

	KernelInfo *snap.Info
	Model      *asserts.Model

	//TODO:UC20: provide bootchains and a way to track measured
	//boot-assets
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
	hasHook, err := HasFDESetupHook()
	if err != nil {
		return fmt.Errorf("cannot check for fde-setup hook %v", err)
	}
	if hasHook {
		return sealKeyToModeenvUsingFdeSetupHook(key, saveKey, model, modeenv)
	}

	return sealKeyToModeenvUsingSecboot(key, saveKey, model, modeenv)
}

func sealKeyToModeenvUsingFdeSetupHook(key, saveKey secboot.EncryptionKey, model *asserts.Model, modeenv *Modeenv) error {
	return fmt.Errorf("cannot use fde-setup hook yet")
}

func sealKeyToModeenvUsingSecboot(key, saveKey secboot.EncryptionKey, model *asserts.Model, modeenv *Modeenv) error {
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

	recoveryBootChains, err := recoveryBootChainsForSystems([]string{modeenv.RecoverySystem}, tbl, model, modeenv)
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
	cmdline, err := ComposeCandidateCommandLine(model)
	if err != nil {
		return fmt.Errorf("cannot compose the candidate command line: %v", err)
	}

	runModeBootChains, err := runModeBootChains(rbl, bl, model, modeenv, cmdline)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChains...))

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

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

	if err := stampSealedKeys(InstallHostWritableDir); err != nil {
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
	keys := []secboot.SealKeyRequest{
		{
			Key:     key,
			KeyFile: filepath.Join(InitramfsBootEncryptionKeyDir, "ubuntu-data.sealed-key"),
		},
	}
	if err := secbootSealKeys(keys, sealKeyParams); err != nil {
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
	keys := []secboot.SealKeyRequest{
		{
			Key:     key,
			KeyFile: filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-data.recovery.sealed-key"),
		},
		{
			Key:     saveKey,
			KeyFile: filepath.Join(InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		},
	}
	if err := secbootSealKeys(keys, sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the fallback encryption keys: %v", err)
	}

	return nil
}

func stampSealedKeys(rootdir string) error {
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return fmt.Errorf("cannot create device fde state directory: %v", err)
	}

	if err := osutil.AtomicWriteFile(stamp, nil, 0644, 0); err != nil {
		return fmt.Errorf("cannot create fde sealed keys stamp file: %v", err)
	}
	return nil
}

// hasSealedKeys return whether any keys were sealed at all
func hasSealedKeys(rootdir string) bool {
	// TODO:UC20: consider more than the marker for cases where we reseal
	// outside of run mode
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	return osutil.FileExists(stamp)
}

// resealKeyToModeenv reseals the existing encryption key to the
// parameters specified in modeenv.
func resealKeyToModeenv(rootdir string, model *asserts.Model, modeenv *Modeenv, expectReseal bool) error {
	if !hasSealedKeys(rootdir) {
		// nothing to do
		return nil
	}

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
	recoveryBootChains, err := recoveryBootChainsForSystems(modeenv.CurrentRecoverySystems, tbl, model, modeenv)
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
	cmdline, err := ComposeCommandLine(model)
	if err != nil {
		return fmt.Errorf("cannot compose the run mode command line: %v", err)
	}

	runModeBootChains, err := runModeBootChains(rbl, bl, model, modeenv, cmdline)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	// reseal the run object
	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChains...))

	needed, nextCount, err := isResealNeeded(pbc, bootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if !needed {
		logger.Debugf("reseal not necessary")
		return nil
	}
	pbcJSON, _ := json.Marshal(pbc)
	logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
	authKeyFile := filepath.Join(saveFDEDir, "tpm-policy-auth-key")
	if err := resealRunObjectKeys(pbc, authKeyFile, roleToBlName); err != nil {
		return err
	}
	logger.Debugf("resealing (%d) succeeded", nextCount)

	bootChainsPath := bootChainsFileUnder(rootdir)
	if err := writeBootChains(pbc, bootChainsPath, nextCount); err != nil {
		return err
	}

	// reseal the fallback object
	rpbc := toPredictableBootChains(recoveryBootChains)

	var nextFallbackCount int
	needed, nextFallbackCount, err = isResealNeeded(rpbc, recoveryBootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if !needed {
		logger.Debugf("fallback reseal not necessary")
		return nil
	}

	rpbcJSON, _ := json.Marshal(rpbc)
	logger.Debugf("resealing (%d) to recovery boot chains: %s", nextCount, rpbcJSON)

	if err := resealFallbackObjectKeys(rpbc, authKeyFile, roleToBlName); err != nil {
		return err
	}
	logger.Debugf("fallback resealing (%d) succeeded", nextFallbackCount)

	recoveryBootChainsPath := recoveryBootChainsFileUnder(rootdir)
	return writeBootChains(rpbc, recoveryBootChainsPath, nextFallbackCount)
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

func recoveryBootChainsForSystems(systems []string, trbl bootloader.TrustedAssetsBootloader, model *asserts.Model, modeenv *Modeenv) (chains []bootChain, err error) {
	for _, system := range systems {
		// get the command line
		cmdline, err := ComposeRecoveryCommandLine(model, system)
		if err != nil {
			return nil, fmt.Errorf("cannot obtain recovery kernel command line: %v", err)
		}

		// get kernel information from seed
		perf := timings.New(nil)
		_, snaps, err := seedReadSystemEssential(dirs.SnapSeedDir, system, []snap.Type{snap.TypeKernel}, perf)
		if err != nil {
			return nil, err
		}
		if len(snaps) != 1 {
			return nil, fmt.Errorf("cannot obtain recovery kernel snap")
		}
		seedKernel := snaps[0]

		var kernelRev string
		if seedKernel.SideInfo.Revision.Store() {
			kernelRev = seedKernel.SideInfo.Revision.String()
		}

		recoveryBootChain, err := trbl.RecoveryBootChain(seedKernel.Path)
		if err != nil {
			return nil, err
		}

		// get asset chains
		assetChain, kbf, err := buildBootAssets(recoveryBootChain, modeenv)
		if err != nil {
			return nil, err
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
			model:          model,
			kernelBootFile: kbf,
		})
	}
	return chains, nil
}

func runModeBootChains(rbl, bl bootloader.Bootloader, model *asserts.Model, modeenv *Modeenv, cmdline string) ([]bootChain, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}

	chains := make([]bootChain, 0, len(modeenv.CurrentKernels))
	for _, k := range modeenv.CurrentKernels {
		info, err := snap.ParsePlaceInfoFromSnapFileName(k)
		if err != nil {
			return nil, err
		}
		runModeBootChain, err := tbl.BootChain(bl, info.MountFile())
		if err != nil {
			return nil, err
		}

		// get asset chains
		assetChain, kbf, err := buildBootAssets(runModeBootChain, modeenv)
		if err != nil {
			return nil, err
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
			KernelCmdlines: []string{cmdline},
			model:          model,
			kernelBootFile: kbf,
		})
	}
	return chains, nil
}

// buildBootAssets takes the BootFiles of a bootloader boot chain and
// produces corresponding bootAssets with the matching current asset
// hashes from modeenv plus it returns separately the last BootFile
// which is for the kernel.
func buildBootAssets(bootFiles []bootloader.BootFile, modeenv *Modeenv) (assets []bootAsset, kernel bootloader.BootFile, err error) {
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
	modelToParams := map[*asserts.Model]*secboot.SealKeyModelParams{}
	modelParams := make([]*secboot.SealKeyModelParams, 0, len(pbc))

	for _, bc := range pbc {
		loadChains, err := bootAssetsToLoadChains(bc.AssetChain, bc.kernelBootFile, roleToBlName)
		if err != nil {
			return nil, fmt.Errorf("cannot build load chains with current boot assets: %s", err)
		}

		// group parameters by model, reuse an existing SealKeyModelParams
		// if the model is the same.
		if params, ok := modelToParams[bc.model]; ok {
			params.KernelCmdlines = strutil.SortedListsUniqueMerge(params.KernelCmdlines, bc.KernelCmdlines)
			params.EFILoadChains = append(params.EFILoadChains, loadChains...)
		} else {
			param := &secboot.SealKeyModelParams{
				Model:          bc.model,
				KernelCmdlines: bc.KernelCmdlines,
				EFILoadChains:  loadChains,
			}
			modelParams = append(modelParams, param)
			modelToParams[bc.model] = param
		}
	}

	return modelParams, nil
}

// isResealNeeded returns true when the predictable boot chains provided as
// input do not match the cached boot chains on disk under rootdir.
// It also returns the next value for the reasel count that is saved
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
