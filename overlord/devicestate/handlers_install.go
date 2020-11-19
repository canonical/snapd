// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"os"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/sysconfig"
)

var (
	bootMakeBootable = boot.MakeBootable
	installRun       = install.Run

	sysconfigConfigureTargetSystem = sysconfig.ConfigureTargetSystem
)

func setSysconfigCloudOptions(opts *sysconfig.Options, gadgetDir string, model *asserts.Model) {
	ubuntuSeedCloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")

	switch {
	// if the gadget has a cloud.conf file, always use that regardless of grade
	case sysconfig.HasGadgetCloudConf(gadgetDir):
		// this is implicitly handled by ConfigureTargetSystem when it configures
		// cloud-init if none of the other options are set, so just break here
		opts.AllowCloudInit = true

	// next thing is if are in secured grade and didn't have gadget config, we
	// disable cloud-init always, clouds should have their own config via
	// gadgets for grade secured
	case model.Grade() == asserts.ModelSecured:
		opts.AllowCloudInit = false

	// TODO:UC20: on grade signed, allow files from ubuntu-seed, but do
	//            filtering on the resultant cloud config

	// next if we are grade dangerous, then we also install cloud configuration
	// from ubuntu-seed if it exists
	case model.Grade() == asserts.ModelDangerous && osutil.IsDirectory(ubuntuSeedCloudCfg):
		opts.AllowCloudInit = true
		opts.CloudInitSrcDir = ubuntuSeedCloudCfg

	// note that if none of the conditions were true, it means we are on grade
	// dangerous or signed, and cloud-init is still allowed to run without
	// additional configuration on first-boot, so that NoCloud CIDATA can be
	// provided for example
	default:
		opts.AllowCloudInit = true
	}
}

func writeModel(model *asserts.Model, where string) error {
	f, err := os.OpenFile(where, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return asserts.NewEncoder(f).Encode(model)
}

func (m *DeviceManager) doSetupRunSystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(t)
	defer perfTimings.Save(st)

	// get gadget dir
	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %v", err)
	}
	gadgetInfo, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}
	gadgetDir := gadgetInfo.MountDir()

	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get kernel info: %v", err)
	}

	modeEnv, err := maybeReadModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}

	// bootstrap
	bopts := install.Options{
		Mount: true,
	}
	useEncryption, err := checkEncryption(st, deviceCtx)
	if err != nil {
		return err
	}
	bopts.Encrypt = useEncryption

	// make sure that gadget is usable for the set up we want to use it in
	gadgetContaints := gadget.ValidationConstraints{
		EncryptedData: useEncryption,
	}
	if err := gadget.Validate(gadgetDir, deviceCtx.Model(), &gadgetContaints); err != nil {
		return fmt.Errorf("cannot use gadget: %v", err)
	}

	var trustedInstallObserver *boot.TrustedAssetsInstallObserver
	// get a nice nil interface by default
	var installObserver gadget.ContentObserver
	trustedInstallObserver, err = boot.TrustedAssetsInstallObserverForModel(deviceCtx.Model(), gadgetDir, useEncryption)
	if err != nil && err != boot.ErrObserverNotApplicable {
		return fmt.Errorf("cannot setup asset install observer: %v", err)
	}
	if err == nil {
		installObserver = trustedInstallObserver
		if !useEncryption {
			// there will be no key sealing, so past the
			// installation pass no other methods need to be called
			trustedInstallObserver = nil
		}
	}

	var installedSystem *install.InstalledSystemSideData
	// run the create partition code
	logger.Noticef("create and deploy partitions")
	func() {
		st.Unlock()
		defer st.Lock()
		installedSystem, err = installRun(gadgetDir, "", bopts, installObserver)
	}()
	if err != nil {
		return fmt.Errorf("cannot install system: %v", err)
	}

	if trustedInstallObserver != nil {
		// sanity check
		if installedSystem.KeysForRoles == nil || installedSystem.KeysForRoles[gadget.SystemData] == nil || installedSystem.KeysForRoles[gadget.SystemSave] == nil {
			return fmt.Errorf("internal error: system encryption keys are unset")
		}
		dataKeySet := installedSystem.KeysForRoles[gadget.SystemData]
		saveKeySet := installedSystem.KeysForRoles[gadget.SystemSave]

		// make note of the encryption keys
		trustedInstallObserver.ChosenEncryptionKeys(dataKeySet.Key, saveKeySet.Key)

		// keep track of recovery assets
		if err := trustedInstallObserver.ObserveExistingTrustedRecoveryAssets(boot.InitramfsUbuntuSeedDir); err != nil {
			return fmt.Errorf("cannot observe existing trusted recovery assets: err")
		}
		if err := saveKeys(installedSystem.KeysForRoles); err != nil {
			return err
		}
		// write markers containing a secret to pair data and save
		if err := writeMarkers(); err != nil {
			return err
		}
	}

	// keep track of the model we installed
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}
	err = writeModel(deviceCtx.Model(), filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir, GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, deviceCtx.Model())
	if err := sysconfigConfigureTargetSystem(opts); err != nil {
		return err
	}

	// make it bootable
	logger.Noticef("make system bootable")
	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	recoverySystemDir := filepath.Join("/systems", modeEnv.RecoverySystem)
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		RecoverySystemDir: recoverySystemDir,
		UnpackedGadgetDir: gadgetDir,
	}
	rootdir := dirs.GlobalRootDir
	if err := bootMakeBootable(deviceCtx.Model(), rootdir, bootWith, trustedInstallObserver); err != nil {
		return fmt.Errorf("cannot make run system bootable: %v", err)
	}

	// request a restart as the last action after a successful install
	logger.Noticef("request system restart")
	st.RequestRestart(state.RestartSystemNow)

	return nil
}

// writeMarkers writes markers containing the same secret to pair data and save.
func writeMarkers() error {
	// ensure directory for markers exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir, 0755); err != nil {
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

	dataMarker := filepath.Join(boot.InstallHostFDEDataDir, "marker")
	if err := osutil.AtomicWriteFile(dataMarker, markerSecret, 0600, 0); err != nil {
		return err
	}

	saveMarker := filepath.Join(boot.InstallHostFDESaveDir, "marker")
	if err := osutil.AtomicWriteFile(saveMarker, markerSecret, 0600, 0); err != nil {
		return err
	}

	return nil
}

func saveKeys(keysForRoles map[string]*install.EncryptionKeySet) error {
	dataKeySet := keysForRoles[gadget.SystemData]

	// ensure directory for keys exists
	if err := os.MkdirAll(boot.InstallHostFDEDataDir, 0755); err != nil {
		return err
	}

	// Write the recovery key
	recoveryKeyFile := filepath.Join(boot.InstallHostFDEDataDir, "recovery.key")
	if err := dataKeySet.RecoveryKey.Save(recoveryKeyFile); err != nil {
		return fmt.Errorf("cannot store recovery key: %v", err)
	}

	saveKeySet := keysForRoles[gadget.SystemSave]
	if saveKeySet == nil {
		// no system-save support
		return nil
	}

	saveKey := filepath.Join(boot.InstallHostFDEDataDir, "ubuntu-save.key")
	reinstallSaveKey := filepath.Join(boot.InstallHostFDEDataDir, "reinstall.key")

	if err := saveKeySet.Key.Save(saveKey); err != nil {
		return fmt.Errorf("cannot store system save key: %v", err)
	}
	if err := saveKeySet.RecoveryKey.Save(reinstallSaveKey); err != nil {
		return fmt.Errorf("cannot store reinstall key: %v", err)
	}
	return nil
}

var secbootCheckKeySealingSupported = secboot.CheckKeySealingSupported

// checkEncryption verifies whether encryption should be used based on the
// model grade and the availability of a TPM device.
func checkEncryption(st *state.State, deviceCtx snapstate.DeviceContext) (res bool, err error) {
	model := deviceCtx.Model()
	secured := model.Grade() == asserts.ModelSecured
	dangerous := model.Grade() == asserts.ModelDangerous

	// check if we should disable encryption non-secured devices
	// TODO:UC20: this is not the final mechanism to bypass encryption
	if dangerous && osutil.FileExists(filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")) {
		return false, nil
	}

	// check if kernel has fde-setup hook based encryption support
	if kernelInfo, err := snapstate.KernelInfo(st, deviceCtx); err == nil {
		// XXX: should we run the fde-setup hook with
		//      "op":"available" or similar now?
		if hasFDESetupHookInKernel(kernelInfo) {
			return true, nil
		}
	}

	// encryption is required in secured devices and optional in other grades
	if err := secbootCheckKeySealingSupported(); err != nil {
		if secured {
			return false, fmt.Errorf("cannot encrypt secured device: %v", err)
		}
		return false, nil
	}

	return true, nil
}
