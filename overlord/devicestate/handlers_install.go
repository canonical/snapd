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
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/sysconfig"
)

var (
	bootMakeBootable            = boot.MakeBootable
	sysconfigConfigureRunSystem = sysconfig.ConfigureRunSystem
	installRun                  = install.Run
)

func setSysconfigCloudOptions(opts *sysconfig.Options, gadgetDir string, model *asserts.Model) {
	// TODO: add support for a single cloud-init `cloud.conf` file
	//       that is then passed to sysconfig

	// check cloud.cfg.d on the ubuntu-seed partition
	//
	// Support custom cloud.cfg.d/*.cfg files on the ubuntu-seed partition
	// during install when in grade "dangerous".
	//
	// XXX: maybe move policy decision into configureRunSystem later?
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	if osutil.IsDirectory(cloudCfg) && model.Grade() == asserts.ModelDangerous {
		opts.CloudInitSrcDir = cloudCfg
		return
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
	kernelDir := kernelInfo.MountDir()

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
	useEncryption, err := checkEncryption(deviceCtx.Model())
	if err != nil {
		return err
	}

	var sealingContentObserver *boot.TrustedAssetsInstallObserver
	if useEncryption {
		fdeDir := "var/lib/snapd/device/fde"
		// ensure directories
		for _, p := range []string{boot.InitramfsEncryptionKeyDir, filepath.Join(boot.InstallHostWritableDir, fdeDir)} {
			if err := os.MkdirAll(p, 0755); err != nil {
				return err
			}
		}

		bopts.Encrypt = true
		bopts.KeyFile = filepath.Join(boot.InitramfsEncryptionKeyDir, "ubuntu-data.sealed-key")
		bopts.RecoveryKeyFile = filepath.Join(boot.InstallHostWritableDir, fdeDir, "recovery.key")
		bopts.TPMLockoutAuthFile = filepath.Join(boot.InstallHostWritableDir, fdeDir, "tpm-lockout-auth")
		bopts.TPMPolicyUpdateDataFile = filepath.Join(boot.InstallHostWritableDir, fdeDir, "policy-update-data")
		bopts.KernelPath = filepath.Join(kernelDir, "kernel.efi")
		bopts.Model = deviceCtx.Model()
		bopts.SystemLabel = modeEnv.RecoverySystem

		sealingContentObserver = boot.NewTrustedAssetsInstallObserver(deviceCtx.Model())
	}

	// run the create partition code
	logger.Noticef("create and deploy partitions")
	func() {
		st.Unlock()
		defer st.Lock()
		err = installRun(gadgetDir, kernelDir, "", bopts, sealingContentObserver)
	}()
	if err != nil {
		return fmt.Errorf("cannot create partitions: %v", err)
	}

	// keep track of the model we installed
	err = writeModel(deviceCtx.Model(), filepath.Join(boot.InitramfsUbuntuBootDir, "model"))
	if err != nil {
		return fmt.Errorf("cannot store the model: %v", err)
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InstallHostWritableDir, GadgetDir: gadgetDir}
	// configure cloud init
	setSysconfigCloudOptions(opts, gadgetDir, deviceCtx.Model())
	if err := sysconfigConfigureRunSystem(opts); err != nil {
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
	if err := bootMakeBootable(deviceCtx.Model(), rootdir, bootWith, sealingContentObserver); err != nil {
		return fmt.Errorf("cannot make run system bootable: %v", err)
	}

	// request a restart as the last action after a successful install
	logger.Noticef("request system restart")
	st.RequestRestart(state.RestartSystemNow)

	return nil
}

var secbootCheckKeySealingSupported = secboot.CheckKeySealingSupported

// checkEncryption verifies whether encryption should be used based on the
// model grade and the availability of a TPM device.
func checkEncryption(model *asserts.Model) (res bool, err error) {
	secured := model.Grade() == asserts.ModelSecured
	dangerous := model.Grade() == asserts.ModelDangerous

	// check if we should disable encryption non-secured devices
	// TODO:UC20: this is not the final mechanism to bypass encryption
	if dangerous && osutil.FileExists(filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")) {
		return false, nil
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
