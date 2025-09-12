// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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

// Package install implements installation logic details for UC20+ systems.  It
// is meant for use by overlord/devicestate and the single-reboot installation
// code in snap-bootstrap.
package install

import (
	"bytes"
	"context"
	"crypto"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	gadgetInstall "github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/timings"
)

// EncryptionSupportInfo describes what encryption is available and needed
// for the current device.
type EncryptionSupportInfo struct {
	// Disabled is set to true if encryption was forcefully
	// disabled (e.g. via the seed partition), if set the rest
	// of the struct content is not relevant.
	Disabled bool

	// StorageSafety describes the level safety properties
	// requested by the model
	StorageSafety asserts.StorageSafety
	// Available is set to true if encryption is available on this device
	// with the used gadget.
	Available bool

	// Type is set to the EncryptionType that can be used if
	// Available is true.
	Type device.EncryptionType

	// UnvailableErr is set if the encryption support availability of
	// the this device and used gadget do not match the
	// storage safety requirements.
	UnavailableErr error

	// UnavailbleWarning describes why encryption support is not
	// available in case it is optional.
	UnavailableWarning string

	// AvailabilityCheckErrors holds details about encryption
	// availability errors identified during preinstall check.
	AvailabilityCheckErrors []secboot.PreinstallErrorDetails

	// availabilityCheckContext holds the configuration and state captured during
	// the preinstall check. It is required for performing follow-up checks with
	// actions to resolve identified errors. It is also an indicator that the
	// preinstall check was used instead of the general availability check.
	availabilityCheckContext *secboot.PreinstallCheckContext

	// PassphraseAuthAvailable is set if the passphrase authentication
	// is supported.
	PassphraseAuthAvailable bool

	// PINAuthAvailable is set if the pin authentication is supported.
	PINAuthAvailable bool
}

// CheckContext returns the underlying preinstall check context.
func (esi *EncryptionSupportInfo) CheckContext() *secboot.PreinstallCheckContext {
	return esi.availabilityCheckContext
}

// ComponentSeedInfo contains information for a component from the seed and
// from its metadata.
type ComponentSeedInfo struct {
	Info *snap.ComponentInfo
	Seed *seed.Component
}

// KernelBootInfo contains information related to the kernel used on installation.
type KernelBootInfo struct {
	KSnapInfo     *gadgetInstall.KernelSnapInfo
	BootableKMods []boot.BootableKModsComponents
}

// SystemSnapdVersions describes the snapd versions in a given systems.
type SystemSnapdVersions struct {
	// SnapdVersion is the version of snapd in a given system
	SnapdVersion string
	// SnapdInitramfsVersion is the version of snapd related component, which participates
	// in the boot process and performs unlocking. Typically snap-bootstrap in the kernel snap.
	SnapdInitramfsVersion string
}

var (
	timeNow = time.Now

	secbootCheckTPMKeySealingSupported = secboot.CheckTPMKeySealingSupported
	secbootPreinstallCheck             = secboot.PreinstallCheck
	secbootPreinstallCheckAction       = (*secboot.PreinstallCheckContext).PreinstallCheckAction
	secbootSaveCheckResult             = (*secboot.PreinstallCheckContext).SaveCheckResult
	secbootFDEOpteeTAPresent           = secboot.FDEOpteeTAPresent
	preinstallCheckTimeout             = 2 * time.Minute

	sysconfigConfigureTargetSystem = sysconfig.ConfigureTargetSystem

	bootUseTokens = boot.UseTokens
)

// BuildKernelBootInfoOpts contains options for BuildKernelBootInfo.
type BuildKernelBootInfoOpts struct {
	// IsCore is true for UC, and false for hybrid systems
	IsCore bool
	// NeedsDriversTree is true if we need a drivers tree (UC/hybrid 24+)
	NeedsDriversTree bool
}

// BuildKernelBootInfo constructs a KernelBootInfo.
func BuildKernelBootInfo(kernInfo *snap.Info, compSeedInfos []ComponentSeedInfo, kernMntPoint string, mntPtForComps map[string]string, opts BuildKernelBootInfoOpts) KernelBootInfo {
	bootKMods := make([]boot.BootableKModsComponents, 0, len(compSeedInfos))
	modulesComps := make([]gadgetInstall.KernelModulesComponentInfo, 0, len(compSeedInfos))
	for _, compSeedInfo := range compSeedInfos {
		ci := compSeedInfo.Info
		if ci.Type == snap.KernelModulesComponent {
			cpi := snap.MinimalComponentContainerPlaceInfo(ci.Component.ComponentName,
				ci.Revision, kernInfo.SnapName())
			modulesComps = append(modulesComps, gadgetInstall.KernelModulesComponentInfo{
				Name:       ci.Component.ComponentName,
				Revision:   ci.Revision,
				MountPoint: mntPtForComps[ci.FullName()],
			})
			bootKMods = append(bootKMods, boot.BootableKModsComponents{
				CompPlaceInfo: cpi,
				CompPath:      compSeedInfo.Seed.Path,
			})
		}
	}

	kSnapInfo := &gadgetInstall.KernelSnapInfo{
		Name:             kernInfo.SnapName(),
		Revision:         kernInfo.Revision,
		MountPoint:       kernMntPoint,
		IsCore:           opts.IsCore,
		ModulesComps:     modulesComps,
		NeedsDriversTree: opts.NeedsDriversTree,
	}

	return KernelBootInfo{
		KSnapInfo:     kSnapInfo,
		BootableKMods: bootKMods,
	}
}

// SetAvailabilityCheckContext is a test only helper for populating EncryptionSupportInfo field availabilityCheckContext.
func (esi *EncryptionSupportInfo) SetAvailabilityCheckContext(checkContext *secboot.PreinstallCheckContext) {
	osutil.MustBeTestBinary("secbootPreinstallCheck can only be used in tests")
	esi.availabilityCheckContext = checkContext
}

// MockSecbootCheckTPMKeySealingSupported mocks secbootCheckTPMKeySealingSupported usage by the package for testing.
func MockSecbootCheckTPMKeySealingSupported(f func(tpmMode secboot.TPMProvisionMode) error) (restore func()) {
	osutil.MustBeTestBinary("secbootCheckTPMKeySealingSupported can only be mocked in tests")
	old := secbootCheckTPMKeySealingSupported
	secbootCheckTPMKeySealingSupported = f
	return func() {
		secbootCheckTPMKeySealingSupported = old
	}
}

// MockSecbootPreinstallCheck mocks secbootPreinstallCheck usage by the package for testing.
func MockSecbootPreinstallCheck(f func(ctx context.Context, bootImagePaths []string) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootPreinstallCheck can only be mocked in tests")
	old := secbootPreinstallCheck
	secbootPreinstallCheck = f
	return func() {
		secbootPreinstallCheck = old
	}
}

// MockSecbootPreinstallCheckAction mocks secbootPreinstallCheckAction usage by the package for testing.
func MockSecbootPreinstallCheckAction(f func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootPreinstallCheckAction can only be mocked in tests")
	old := secbootPreinstallCheckAction
	secbootPreinstallCheckAction = f
	return func() {
		secbootPreinstallCheckAction = old
	}
}

// MockSecbootSaveCheckResult mocks secbootSaveCheckResult usage by the package for testing.
func MockSecbootSaveCheckResult(f func(pcc *secboot.PreinstallCheckContext, filename string) error) (restore func()) {
	osutil.MustBeTestBinary("secbootSaveCheckResult can only be mocked in tests")
	old := secbootSaveCheckResult
	secbootSaveCheckResult = f
	return func() {
		secbootSaveCheckResult = old
	}
}

func checkPassphraseSupportedByTargetSystem(sysVer SystemSnapdVersions) (bool, error) {
	const minSnapdVersion = "2.68"
	if sysVer.SnapdVersion == "" || sysVer.SnapdInitramfsVersion == "" {
		return false, nil
	}

	// snapd snap must support passphrases.
	cmp, err := strutil.VersionCompare(sysVer.SnapdVersion, minSnapdVersion)
	if err != nil {
		return false, fmt.Errorf("invalid snapd version in info file from snapd snap: %v", err)
	}
	if cmp < 0 {
		return false, nil
	}
	// snap-bootstrap inside the kernel must support passphrases.
	cmp, err = strutil.VersionCompare(sysVer.SnapdInitramfsVersion, minSnapdVersion)
	if err != nil {
		return false, fmt.Errorf("invalid snapd version in info file from kernel snap: %v", err)
	}
	if cmp < 0 {
		return false, nil
	}

	return true, nil
}

// EncryptionConstraints is the set of constraints that
// [GetEncryptionSupportInfo] must consider when deciding how to encrypt the
// system.
type EncryptionConstraints struct {
	Model         *asserts.Model
	Kernel        *snap.Info
	Gadget        *gadget.Info
	TPMMode       secboot.TPMProvisionMode
	SnapdVersions SystemSnapdVersions
	CheckContext  *secboot.PreinstallCheckContext
	CheckAction   *secboot.PreinstallAction
}

// GetEncryptionSupportInfo returns the encryption support information
// for the given model, TPM provision mode, kernel and gadget information and
// system hardware. It uses runSetupHook to invoke the kernel fde-setup hook if
// any is available, leaving the caller to decide how, based on the environment.
func GetEncryptionSupportInfo(constraints EncryptionConstraints, runSetupHook fde.RunSetupHookFunc) (EncryptionSupportInfo, error) {
	secured := constraints.Model.Grade() == asserts.ModelSecured
	dangerous := constraints.Model.Grade() == asserts.ModelDangerous
	encrypted := constraints.Model.StorageSafety() == asserts.StorageSafetyEncrypted

	res := EncryptionSupportInfo{
		StorageSafety: constraints.Model.StorageSafety(),
	}

	// check if we should disable encryption non-secured devices
	// TODO:UC20: this is not the final mechanism to bypass encryption
	if dangerous && osutil.FileExists(filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")) {
		res.Disabled = true
		return res, nil
	}

	// check encryption: this can either be provided by one of three mechanisms,
	// and they are checked in this order:
	// - the fde-setup hook
	// - the optee trusted application
	// - the built-in secboot based encryption

	checkFDESetupHookEncryption := hasFDESetupHookInKernel(constraints.Kernel)
	// unlike the fde-setup hook, we don't have a way to statically verify that
	// we should use optee. thus, we check for its presence and fall back to the
	// tpm if we can't see optee. this means that this function won't return any
	// optee-specific errors/warnings. change this?
	checkOPTEEEncryption := !checkFDESetupHookEncryption && secbootFDEOpteeTAPresent()
	checkSecbootEncryption := !checkOPTEEEncryption

	var checkEncryptionErr error
	switch {
	case checkFDESetupHookEncryption:
		res.Type, checkEncryptionErr = checkFDEFeatures(runSetupHook)
	case checkOPTEEEncryption:
		res.Type = device.EncryptionTypeLUKS
	case checkSecbootEncryption:
		preinstallCheckContext, unavailableReason, preinstallErrorDetails, err := encryptionAvailabilityCheck(
			constraints.CheckContext,
			constraints.CheckAction,
			constraints.Model,
			constraints.TPMMode,
		)
		if err != nil {
			return res, fmt.Errorf("internal error: cannot perform secboot encryption check: %v", err)
		}
		res.availabilityCheckContext = preinstallCheckContext

		if unavailableReason == "" {
			res.Type = device.EncryptionTypeLUKS
		} else {
			checkEncryptionErr = errors.New(unavailableReason)
			res.AvailabilityCheckErrors = preinstallErrorDetails
		}
	default:
		return res, fmt.Errorf("internal error: no encryption checked in encryptionSupportInfo")
	}
	res.Available = checkEncryptionErr == nil

	if checkEncryptionErr != nil {
		switch {
		case secured:
			res.UnavailableErr = fmt.Errorf("cannot encrypt device storage as mandated by model grade secured: %v", checkEncryptionErr)
		case encrypted:
			res.UnavailableErr = fmt.Errorf("cannot encrypt device storage as mandated by encrypted storage-safety model option: %v", checkEncryptionErr)
		case checkFDESetupHookEncryption:
			res.UnavailableWarning = fmt.Sprintf("not encrypting device storage as querying kernel fde-setup hook did not succeed: %v", checkEncryptionErr)
		case checkSecbootEncryption:
			res.UnavailableWarning = fmt.Sprintf("not encrypting device storage as checking TPM gave: %v", checkEncryptionErr)
		default:
			return res, fmt.Errorf("internal error: checkEncryptionErr is set but not handled by the code")
		}
	}

	// If encryption is available check if the gadget is
	// compatible with encryption.
	if res.Available {
		// Passphrase support is only available for TPM based encryption for now.
		// Hook based setup support does not make sense (at least for now) because
		// it is usually in the context of embedded systems where passphrase
		// authentication is not practical.
		if checkSecbootEncryption {
			passphraseAuthAvailable, err := checkPassphraseSupportedByTargetSystem(constraints.SnapdVersions)
			if err != nil {
				return res, fmt.Errorf("cannot check passphrase support: %v", err)
			}
			res.PassphraseAuthAvailable = passphraseAuthAvailable
		}
		opts := &gadget.ValidationConstraints{
			EncryptedData: true,
		}
		if err := gadget.Validate(constraints.Gadget, constraints.Model, opts); err != nil {
			if secured || encrypted {
				res.UnavailableErr = fmt.Errorf("cannot use encryption with the gadget: %v", err)
			} else {
				res.UnavailableWarning = fmt.Sprintf("cannot use encryption with the gadget, disabling encryption: %v", err)
			}
			res.Available = false
			res.Type = device.EncryptionTypeNone
		}
	}

	return res, nil
}

func encryptionAvailabilityCheck(
	checkContext *secboot.PreinstallCheckContext,
	checkAction *secboot.PreinstallAction,
	model *asserts.Model,
	tpmMode secboot.TPMProvisionMode,
) (*secboot.PreinstallCheckContext, string, []secboot.PreinstallErrorDetails, error) {
	supported, err := preinstallCheckSupportedWithEnvFallback(model)
	if err != nil {
		return nil, "", nil, fmt.Errorf("cannot confirm preinstall check support: %v", err)
	}
	if supported {
		// use comprehensive preinstall check
		images, err := orderedCurrentBootImages(model)
		if err != nil {
			return nil, "", nil, fmt.Errorf("cannot locate ordered current boot images: %v", err)
		}

		if checkAction != nil && checkContext == nil {
			return nil, "", nil, errors.New("cannot use preinstall check action without context")
		}

		ctx, cancel := context.WithTimeout(context.Background(), preinstallCheckTimeout)
		defer cancel()

		var (
			preinstallErrorDetails []secboot.PreinstallErrorDetails
			newCheckContext        *secboot.PreinstallCheckContext
		)

		if checkContext != nil {
			preinstallErrorDetails, err = secbootPreinstallCheckAction(checkContext, ctx, checkAction)
			newCheckContext = checkContext
		} else {
			newCheckContext, preinstallErrorDetails, err = secbootPreinstallCheck(ctx, images)
		}

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, "", nil, fmt.Errorf("preinstall check timed out: %v", err)
			}
			return nil, "", nil, err
		}

		switch len(preinstallErrorDetails) {
		case 0:
			return newCheckContext, "", nil, nil
		case 1:
			return newCheckContext, preinstallErrorDetails[0].Message, preinstallErrorDetails, nil
		default:
			return newCheckContext, fmt.Sprintf("preinstall check identified %d errors", len(preinstallErrorDetails)), preinstallErrorDetails, nil
		}
	}

	// use general availability check
	err = secbootCheckTPMKeySealingSupported(tpmMode)
	if err != nil {
		return nil, err.Error(), nil, nil
	}
	return nil, "", nil, nil
}

func preinstallCheckSupportedWithEnvFallback(model *asserts.Model) (bool, error) {
	//TODO:FDEM: This temporary fallback must be removed before release of snapd 2.71
	if osutil.GetenvBool("SNAPD_DISABLE_PREINSTALL_CHECK") {
		logger.Noticef(`preinstall check disabled by environment variable "SNAPD_DISABLE_PREINSTALL_CHECK"`)
		return false, nil
	}

	return CheckHybridQuestingRelease(model)
}

// CheckHybridQuestingRelease returns true if the given model and runtime release
// information indicates that this is a hybrid Ubuntu system with a version of
// 25.10 or higher.
func CheckHybridQuestingRelease(model *asserts.Model) (bool, error) {
	if !model.HybridClassic() {
		return false, nil
	}

	if release.ReleaseInfo.ID != "ubuntu" {
		logger.Noticef("unexpected OS release ID %q", release.ReleaseInfo.ID)
		return false, nil
	}

	const minSupportedVersion = "25.10"
	cmp, err := strutil.VersionCompare(release.ReleaseInfo.VersionID, minSupportedVersion)
	if err != nil {
		return false, fmt.Errorf("cannot perform version comparison with OS release version ID: %v", err)
	}

	return cmp >= 0, nil
}

func orderedCurrentBootImages(model *asserts.Model) ([]string, error) {
	if model.HybridClassic() {
		images, err := orderedCurrentBootImagesHybrid()
		if err != nil {
			return nil, fmt.Errorf("cannot locate hybrid system boot images: %v", err)
		}
		return images, nil
	}
	// TODO: consider support for core systems
	return nil, nil
}

func orderedCurrentBootImagesHybrid() ([]string, error) {
	imageInfo := []struct {
		name string
		glob string
	}{
		{"shim", filepath.Join(dirs.GlobalRootDir, "cdrom/EFI/boot/boot*.efi")},
		{"grub", filepath.Join(dirs.GlobalRootDir, "cdrom/EFI/boot/grub*.efi")},
		{"kernel", filepath.Join(dirs.GlobalRootDir, "cdrom/casper/vmlinuz")},
	}

	var bootImagePaths []string
	for _, info := range imageInfo {
		matches, err := filepath.Glob(info.glob)
		if err != nil {
			return nil, fmt.Errorf("cannot use globbing pattern %q: %v", info.glob, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("cannot locate installer %s using globbing pattern %q", info.name, info.glob)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("unexpected multiple matches for installer %s obtained using globbing pattern %q", info.name, info.glob)
		}
		bootImagePaths = append(bootImagePaths, matches[0])
	}

	return bootImagePaths, nil
}

func hasFDESetupHookInKernel(kernelInfo *snap.Info) bool {
	_, ok := kernelInfo.Hooks["fde-setup"]
	return ok
}

func checkFDEFeatures(runSetupHook fde.RunSetupHookFunc) (et device.EncryptionType, err error) {
	// Run fde-setup hook with "op":"features". If the hook
	// returns any {"features":[...]} reply we consider the
	// hardware supported. If the hook errors or if it returns
	// {"error":"hardware-unsupported"} we don't.
	features, err := fde.CheckFeatures(runSetupHook)
	if err != nil {
		return et, err
	}
	switch {
	case strutil.ListContains(features, "inline-crypto-engine"):
		et = device.EncryptionTypeLUKSWithICE
	default:
		et = device.EncryptionTypeLUKS
	}

	return et, nil
}

// CheckEncryptionSupport checks the type of encryption support for disks
// available if any and returns the corresponding device.EncryptionType,
// internally it uses GetEncryptionSupportInfo with the provided parameters.
func CheckEncryptionSupport(
	constraints EncryptionConstraints,
	runSetupHook fde.RunSetupHookFunc,
) (device.EncryptionType, error) {
	res, err := GetEncryptionSupportInfo(constraints, runSetupHook)
	if err != nil {
		return "", err
	}
	if res.UnavailableWarning != "" {
		logger.Noticef("%s", res.UnavailableWarning)
	}
	// encryption disabled or preferred unencrypted: follow the model preferences here even if encryption would be available
	if res.Disabled || res.StorageSafety == asserts.StorageSafetyPreferUnencrypted {
		res.Type = device.EncryptionTypeNone
	}

	return res.Type, res.UnavailableErr
}

// BuildInstallObserver creates an observer for gadget assets if
// applicable, otherwise the returned gadget.ContentObserver is nil.
// The observer if any is also returned as non-nil trustedObserver if
// encryption is in use.
func BuildInstallObserver(model *asserts.Model, gadgetDir string, useEncryption bool) (
	observer gadget.ContentObserver, trustedObserver boot.TrustedAssetsInstallObserver, err error) {

	// observer will be a nil interface by default
	trustedObserver, err = boot.TrustedAssetsInstallObserverForModel(model, gadgetDir, useEncryption)
	if err != nil && err != boot.ErrObserverNotApplicable {
		return nil, nil, fmt.Errorf("cannot setup asset install observer: %v", err)
	}
	if err == nil {
		observer = trustedObserver
		if !useEncryption && !trustedObserver.BootLoaderSupportsEfiVariables() {
			// there will be no key sealing, so past the
			// installation pass no other methods need to be called
			trustedObserver = nil
		}
	}

	return observer, trustedObserver, nil
}

// PrepareEncryptedSystemData executes preparations related to encrypted system data:
// * provides trustedInstallObserver with the chosen keys
// * uses trustedInstallObserver to track any trusted assets in ubuntu-seed
// * save keys and markers for ubuntu-data being able to safely open ubuntu-save
// It is the responsibility of the caller to call
// ObserveExistingTrustedRecoveryAssets on trustedInstallObserver.
func PrepareEncryptedSystemData(
	model *asserts.Model,
	installKeyForRole map[string]secboot.BootstrappedContainer,
	volumesAuth *device.VolumesAuthOptions,
	checkContext *secboot.PreinstallCheckContext,
	trustedInstallObserver boot.TrustedAssetsInstallObserver,
) error {
	// validity check
	if len(installKeyForRole) == 0 || installKeyForRole[gadget.SystemData] == nil || installKeyForRole[gadget.SystemSave] == nil {
		return fmt.Errorf("internal error: system encryption keys are unset")
	}
	dataBootstrappedContainer := installKeyForRole[gadget.SystemData]
	saveBootstrappedContainer := installKeyForRole[gadget.SystemSave]

	var primaryKey []byte

	if saveBootstrappedContainer != nil {
		if bootUseTokens(model) {
			protectorKey, err := keys.NewProtectorKey()
			if err != nil {
				return err
			}

			plainKey, generatedPK, diskKey, err := protectorKey.CreateProtectedKey(nil)
			if err != nil {
				return err
			}

			if err := saveBootstrappedContainer.AddKey("default", diskKey); err != nil {
				return err
			}
			tokenWriter, err := saveBootstrappedContainer.GetTokenWriter("default")
			if err != nil {
				return err
			}
			if err := plainKey.Write(tokenWriter); err != nil {
				return err
			}

			if err := saveKeys(model, protectorKey); err != nil {
				return err
			}

			// This is the "default" key of save partition. So it should also
			// be saved to keyring to have similar state as unlocking in run mode.
			saveBootstrappedContainer.RegisterKeyAsUsed(generatedPK, diskKey)

			primaryKey = generatedPK
		} else {
			saveKey, err := keys.NewEncryptionKey()
			if err != nil {
				return err
			}

			if err := saveBootstrappedContainer.AddKey("default", saveKey); err != nil {
				return err
			}

			if err := saveLegacyKeys(model, saveKey); err != nil {
				return err
			}
		}
	}

	if checkContext != nil {
		// write check result containing information required
		// for optimum PCR configuration and resealing
		if err := saveCheckResult(checkContext); err != nil {
			return err
		}
	}

	// write markers containing a secret to pair data and save
	if err := writeMarkers(model); err != nil {
		return err
	}

	// make note of the encryption keys and auth options
	trustedInstallObserver.SetEncryptionParams(dataBootstrappedContainer, saveBootstrappedContainer, primaryKey, volumesAuth)

	return nil
}

func saveCheckResult(checkContext *secboot.PreinstallCheckContext) error {
	saveCheckResultPath := device.PreinstallCheckResultUnder(boot.InstallHostFDESaveDir)
	return secbootSaveCheckResult(checkContext, saveCheckResultPath)
}

// writeMarkers writes markers containing the same secret to pair data and save.
func writeMarkers(model *asserts.Model) error {
	// ensure directory for markers exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir(model), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(boot.InstallHostFDESaveDir, 0755); err != nil {
		return err
	}

	// generate a secret random marker
	markerSecret, err := randutil.CryptoTokenBytes(32)
	if err != nil {
		return fmt.Errorf("cannot create ubuntu-data/save marker secret: %v", err)
	}

	return device.WriteEncryptionMarkers(boot.InstallHostFDEDataDir(model), boot.InstallHostFDESaveDir, markerSecret)
}

func saveKeys(model *asserts.Model, saveKey keys.ProtectorKey) error {
	saveKeyPath := device.SaveKeyUnder(boot.InstallHostFDEDataDir(model))

	if err := os.MkdirAll(filepath.Dir(saveKeyPath), 0755); err != nil {
		return err
	}

	return saveKey.SaveToFile(saveKeyPath)
}

func saveLegacyKeys(model *asserts.Model, saveKey keys.EncryptionKey) error {
	saveKeyPath := device.SaveKeyUnder(boot.InstallHostFDEDataDir(model))

	if err := os.MkdirAll(filepath.Dir(saveKeyPath), 0755); err != nil {
		return err
	}

	return saveKey.Save(saveKeyPath)
}

// PrepareRunSystemData prepares the run system:
// * it writes the model to ubuntu-boot
// * sets up/copies any allowed and relevant cloud init configuration
// * plus other details
func PrepareRunSystemData(model *asserts.Model, gadgetDir string, perfTimings timings.Measurer) error {
	// keep track of the model we installed
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}
	err = writeModel(model, filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// XXX does this make sense from initramfs?
	// preserve systemd-timesyncd clock timestamp, so that RTC-less devices
	// can start with a more recent time on the next boot
	if err := writeTimesyncdClock(dirs.GlobalRootDir, boot.InstallHostWritableDir(model)); err != nil {
		return fmt.Errorf("cannot seed timesyncd clock: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir(model), GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, model)
	timings.Run(perfTimings, "sysconfig-configure-target-system", "Configure target system", func(timings.Measurer) {
		err = sysconfigConfigureTargetSystem(model, opts)
	})
	if err != nil {
		return err
	}

	// TODO: FIXME: this should go away after we have time to design a proper
	//              solution

	if !model.Classic() {
		// on some specific devices, we need to create these directories in
		// _writable_defaults in order to allow the install-device hook to install
		// some files there, this eventually will go away when we introduce a proper
		// mechanism not using system-files to install files onto the root
		// filesystem from the install-device hook
		if err := fixupWritableDefaultDirs(boot.InstallHostWritableDir(model)); err != nil {
			return err
		}
	}

	return nil
}

func writeModel(model *asserts.Model, where string) error {
	f, err := os.OpenFile(where, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return asserts.NewEncoder(f).Encode(model)
}

func setSysconfigCloudOptions(opts *sysconfig.Options, gadgetDir string, model *asserts.Model) {
	ubuntuSeedCloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")

	grade := model.Grade()

	// we always set the cloud-init src directory if it exists, it is
	// automatically ignored by sysconfig in the case it shouldn't be used
	if osutil.IsDirectory(ubuntuSeedCloudCfg) {
		opts.CloudInitSrcDir = ubuntuSeedCloudCfg
	}

	switch {
	// if the gadget has a cloud.conf file, always use that regardless of grade
	case sysconfig.HasGadgetCloudConf(gadgetDir):
		opts.AllowCloudInit = true

	// next thing is if are in secured grade and didn't have gadget config, we
	// disable cloud-init always, clouds should have their own config via
	// gadgets for grade secured
	case grade == asserts.ModelSecured:
		opts.AllowCloudInit = false

	// all other cases we allow cloud-init to run, either through config that is
	// available at runtime via a CI-DATA USB drive, or via config on
	// ubuntu-seed if that is allowed by the model grade, etc.
	default:
		opts.AllowCloudInit = true
	}
}

func fixupWritableDefaultDirs(systemDataDir string) error {
	// the _writable_default directory is used to put files in place on
	// ubuntu-data from install mode, so we abuse it here for a specific device
	// to let that device install files with system-files and the install-device
	// hook

	// eventually this will be a proper, supported, designed mechanism instead
	// of just this hack, but this hack is just creating the directories, since
	// the system-files interface only allows creating the file, not creating
	// the directories leading up to that file, and since the file is deeply
	// nested we would effectively have to give all permission to the device
	// to create any file on ubuntu-data which we don't want to do, so we keep
	// this restriction to let the device create one specific file, and then
	// we behind the scenes just create the directories for the device

	for _, subDirToCreate := range []string{"/etc/udev/rules.d", "/etc/modprobe.d", "/etc/modules-load.d/", "/etc/systemd/network"} {
		dirToCreate := sysconfig.WritableDefaultsDir(systemDataDir, subDirToCreate)

		if err := os.MkdirAll(dirToCreate, 0755); err != nil {
			return err
		}
	}

	return nil
}

func writeTimesyncdClock(srcRootDir, dstRootDir string) error {
	// keep track of the time
	const timesyncClockInRoot = "/var/lib/systemd/timesync/clock"
	clockSrc := filepath.Join(srcRootDir, timesyncClockInRoot)
	clockDst := filepath.Join(dstRootDir, timesyncClockInRoot)
	if err := os.MkdirAll(filepath.Dir(clockDst), 0755); err != nil {
		return fmt.Errorf("cannot store the clock: %v", err)
	}
	if !osutil.FileExists(clockSrc) {
		logger.Noticef("timesyncd clock timestamp %v does not exist", clockSrc)
		return nil
	}
	// clock file is owned by a specific user/group, thus preserve
	// attributes of the source
	if err := osutil.CopyFile(clockSrc, clockDst, osutil.CopyFlagPreserveAll); err != nil {
		return fmt.Errorf("cannot copy clock: %v", err)
	}
	// the file is empty however, its modification timestamp is used to set
	// up the current time
	if err := os.Chtimes(clockDst, timeNow(), timeNow()); err != nil {
		return fmt.Errorf("cannot update clock timestamp: %v", err)
	}
	return nil
}

func comparePreseedAndSeedSnaps(seedSnap *seed.Snap, preseedSnap *asserts.PreseedSnap) error {
	if preseedSnap.Revision != seedSnap.SideInfo.Revision.N {
		rev := snap.Revision{N: preseedSnap.Revision}
		return fmt.Errorf("snap %q has wrong revision %s (expected: %s)", seedSnap.SnapName(), seedSnap.SideInfo.Revision, rev)
	}
	if preseedSnap.SnapID != seedSnap.SideInfo.SnapID {
		return fmt.Errorf("snap %q has wrong snap id %q (expected: %q)", seedSnap.SnapName(), seedSnap.SideInfo.SnapID, preseedSnap.SnapID)
	}

	expectedComps := make(map[string]asserts.PreseedComponent, len(preseedSnap.Components))
	for _, c := range preseedSnap.Components {
		expectedComps[c.Name] = c
	}

	for _, c := range seedSnap.Components {
		preseedComp, ok := expectedComps[c.CompSideInfo.Component.ComponentName]
		if !ok {
			return fmt.Errorf("component %q not present in the preseed assertion", c.CompSideInfo.Component)
		}

		if preseedComp.Revision != c.CompSideInfo.Revision.N {
			rev := snap.Revision{N: preseedComp.Revision}
			return fmt.Errorf("component %q has wrong revision %s (expected: %s)", c.CompSideInfo.Component, c.CompSideInfo.Revision, rev)
		}

		// once we've seen the component, remove it from the expected
		// components. anything left over is missing from the seed.
		delete(expectedComps, c.CompSideInfo.Component.ComponentName)
	}

	if len(expectedComps) != 0 {
		missing := make([]string, 0, len(expectedComps))
		for name := range expectedComps {
			missing = append(missing, naming.NewComponentRef(seedSnap.SnapName(), name).String())
		}
		return fmt.Errorf("seed is missing components expected by preseed assertion: %s", strutil.Quoted(missing))
	}

	return nil
}

// ApplyPreseededData applies the preseed payload from the given seed, including
// installing snaps, to the given target system filesystem.
func ApplyPreseededData(preseedSeed seed.PreseedCapable, writableDir string) error {
	preseedAs, err := preseedSeed.LoadPreseedAssertion()
	if err != nil {
		return err
	}

	preseedArtifact := preseedSeed.ArtifactPath("preseed.tgz")

	// TODO: consider a writer that feeds the file to stdin of tar and calculates the digest at the same time.
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	if err != nil {
		return fmt.Errorf("cannot calculate preseed artifact digest: %v", err)
	}

	digest, err := base64.RawURLEncoding.DecodeString(preseedAs.ArtifactSHA3_384())
	if err != nil {
		return fmt.Errorf("cannot decode preseed artifact digest")
	}
	if !bytes.Equal(sha3_384, digest) {
		return fmt.Errorf("invalid preseed artifact digest")
	}

	logger.Noticef("apply preseed data: %q, %q", writableDir, preseedArtifact)
	cmd := exec.Command("tar", "--extract", "--preserve-permissions", "--preserve-order", "--gunzip", "--directory", writableDir, "-f", preseedArtifact)
	if err := cmd.Run(); err != nil {
		return err
	}

	logger.Noticef("copying snaps")

	if err := os.MkdirAll(filepath.Join(writableDir, "var/lib/snapd/snaps"), 0755); err != nil {
		return err
	}

	tm := timings.New(nil)
	snapHandler := &preseedSnapHandler{writableDir: writableDir}
	if err := preseedSeed.LoadMeta("run", snapHandler, tm); err != nil {
		return err
	}

	preseedSnaps := make(map[string]*asserts.PreseedSnap)
	for _, ps := range preseedAs.Snaps() {
		preseedSnaps[ps.Name] = ps
	}

	checkSnap := func(ssnap *seed.Snap) error {
		ps, ok := preseedSnaps[ssnap.SnapName()]
		if !ok {
			return fmt.Errorf("snap %q not present in the preseed assertion", ssnap.SnapName())
		}
		return comparePreseedAndSeedSnaps(ssnap, ps)
	}

	esnaps := preseedSeed.EssentialSnaps()
	msnaps, err := preseedSeed.ModeSnaps("run")
	if err != nil {
		return err
	}
	if len(msnaps)+len(esnaps) != len(preseedSnaps) {
		return fmt.Errorf("seed has %d snaps but %d snaps are required by preseed assertion", len(msnaps)+len(esnaps), len(preseedSnaps))
	}

	for _, esnap := range esnaps {
		if err := checkSnap(esnap); err != nil {
			return err
		}
	}

	for _, ssnap := range msnaps {
		if err := checkSnap(ssnap); err != nil {
			return err
		}
	}

	return nil
}

// TODO: consider reusing this kind of handler for UC20 seeding
type preseedSnapHandler struct {
	writableDir string
}

func (p *preseedSnapHandler) HandleUnassertedContainer(cpi snap.ContainerPlaceInfo, path string, _ timings.Measurer) (string, error) {
	targetPath := filepath.Join(p.writableDir, cpi.MountFile())
	mountDir := filepath.Join(p.writableDir, cpi.MountDir())

	sq := squashfs.New(path)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	if _, err := sq.Install(targetPath, mountDir, opts); err != nil {
		return "", fmt.Errorf("cannot install snap %q: %v", cpi.ContainerName(), err)
	}

	return targetPath, nil
}

func (p *preseedSnapHandler) HandleAndDigestAssertedContainer(cpi snap.ContainerPlaceInfo, path string, _ timings.Measurer) (string, string, uint64, error) {
	targetPath := filepath.Join(p.writableDir, cpi.MountFile())
	mountDir := filepath.Join(p.writableDir, cpi.MountDir())

	logger.Debugf("copying: %q to %q; mount dir=%q", path, targetPath, mountDir)

	srcFile, err := os.Open(path)
	if err != nil {
		return "", "", 0, err
	}
	defer srcFile.Close()

	destFile, err := osutil.NewAtomicFile(targetPath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot create atomic file: %v", err)
	}
	defer destFile.Cancel()

	finfo, err := srcFile.Stat()
	if err != nil {
		return "", "", 0, err
	}

	destFile.SetModTime(finfo.ModTime())

	h := crypto.SHA3_384.New()
	w := io.MultiWriter(h, destFile)

	size, err := io.CopyBuffer(w, srcFile, make([]byte, 2*1024*1024))
	if err != nil {
		return "", "", 0, err
	}
	if err := destFile.Commit(); err != nil {
		return "", "", 0, fmt.Errorf("cannot copy snap %q: %v", cpi.ContainerName(), err)
	}

	sq := squashfs.New(targetPath)
	opts := &snap.InstallOptions{MustNotCrossDevices: true}
	// since Install target path is the same as source path passed to squashfs.New,
	// Install isn't going to copy the blob, but we call it to set up mount directory etc.
	if _, err := sq.Install(targetPath, mountDir, opts); err != nil {
		return "", "", 0, fmt.Errorf("cannot install snap %q: %v", cpi.ContainerName(), err)
	}

	sha3_384, err := asserts.EncodeDigest(crypto.SHA3_384, h.Sum(nil))
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot encode snap %q digest: %v", path, err)
	}
	return targetPath, sha3_384, uint64(size), nil
}
