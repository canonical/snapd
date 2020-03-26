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
	"os/exec"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sysconfig"
)

var (
	bootMakeBootable            = boot.MakeBootable
	sysconfigConfigureRunSystem = sysconfig.ConfigureRunSystem
)

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

	args := []string{
		// create partitions missing from the device
		"create-partitions",
		// mount filesystems after they're created
		"--mount",
	}

	useEncryption, err := checkEncryption(deviceCtx.Model())
	if err != nil {
		return err
	}
	if useEncryption {
		args = append(args,
			// enable data encryption
			"--encrypt",
			// location to store the keyfile
			"--key-file", filepath.Join(boot.InitramfsUbuntuBootDir, "ubuntu-data.keyfile.unsealed"),
		)
	}
	args = append(args, gadgetDir)

	// run the create partition code
	st.Unlock()
	output, err := exec.Command(filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), args...).CombinedOutput()
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot create partitions: %v", osutil.OutputErr(output, err))
	}

	// configure the run system
	opts := &sysconfig.Options{TargetRootDir: boot.InitramfsWritableDir}
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	// Support custom cloud.cfg.d/*.cfg files on the ubuntu-seed partition
	// during install when in grade "dangerous". We will support configs
	// from the gadget later too, see sysconfig/cloudinit.go
	//
	// XXX: maybe move policy decision into configureRunSystem later?
	if osutil.IsDirectory(cloudCfg) && deviceCtx.Model().Grade() == asserts.ModelDangerous {
		opts.CloudInitSrcDir = cloudCfg
	}
	if err := sysconfigConfigureRunSystem(opts); err != nil {
		return err
	}

	// make it bootable
	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}

	bootBaseInfo, err := snapstate.BootBaseInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get boot base info: %v", err)
	}
	modeEnv, err := m.readMaybeModeenv()
	if err != nil {
		return err
	}
	if modeEnv == nil {
		return fmt.Errorf("missing modeenv, cannot proceed")
	}
	recoverySystemDir := filepath.Join("/systems", modeEnv.RecoverySystem)
	bootWith := &boot.BootableSet{
		Base:              bootBaseInfo,
		BasePath:          bootBaseInfo.MountFile(),
		Kernel:            kernelInfo,
		KernelPath:        kernelInfo.MountFile(),
		RecoverySystemDir: recoverySystemDir,
	}
	rootdir := dirs.GlobalRootDir
	if err := bootMakeBootable(deviceCtx.Model(), rootdir, bootWith); err != nil {
		return fmt.Errorf("cannot make run system bootable: %v", err)
	}

	// request a restart as the last action after a successful install
	st.RequestRestart(state.RestartSystem)

	return nil
}

// TODO:UC20: set to real TPM availability check function
var checkTPMAvailability = func() error {
	return nil
}

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
	if err := checkTPMAvailability(); err != nil {
		if secured {
			return false, fmt.Errorf("cannot encrypt secured device: %v", err)
		}
		return false, nil
	}

	return true, nil
}
