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
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timings"
)

var bootMakeBootable = boot.MakeBootable

func (m *DeviceManager) doSetupRunSystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := timings.NewForTask(t)
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
		ubuntuBootDir := filepath.Join(dirs.RunMnt, "ubuntu-boot")

		args = append(args,
			// enable data encryption
			"--encrypt",
			// location to store the keyfile
			"--key-file", filepath.Join(ubuntuBootDir, "ubuntu-data.keyfile.unsealed"),
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
	if err := configureRunSystem(deviceCtx); err != nil {
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
	recoverySystemDir := filepath.Join("/systems", m.modeEnv.RecoverySystem)
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
	if dangerous && osutil.FileExists(filepath.Join(dirs.RunMnt, "ubuntu-seed", ".force-unencrypted")) {
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

func configureCloudInit(deviceCtx snapstate.DeviceContext) error {
	// disable cloud-init by default (as it's not confined)

	// 0. TODO:UC20: check gadget cloud.cfg.d/* (with whitelisted keys?)

	// 1. check for grade: dangerous and a custom cloud init
	cloudCfg := filepath.Join(dirs.RunMnt, "ubuntu-seed/cloud.cfg.d")
	if osutil.IsDirectory(cloudCfg) && deviceCtx.Model().Grade() == asserts.ModelDangerous {
		ubuntuDataCloudCfgDir := filepath.Join(dirs.RunMnt, "ubuntu-data/system-data/etc/cloud/cloud.cfg.d/")
		if err := os.MkdirAll(ubuntuDataCloudCfgDir, 0755); err != nil {
			return fmt.Errorf("cannot make cloud config dir: %v", err)
		}
		ccl, err := filepath.Glob(filepath.Join(cloudCfg, "*.cfg"))
		if err != nil {
			return err
		}
		for _, cc := range ccl {
			if err := osutil.CopyFile(cc, filepath.Join(ubuntuDataCloudCfgDir, filepath.Base(cc)), 0); err != nil {
				return err
			}
		}
		return nil
	}

	// 2. TODO:UC20: allow cloud.cfg.d (with whitelisted keys) for non
	//    grade dangerous systems

	// 3. nothing of the above applied, disable cloud-init
	ubuntuDataCloud := filepath.Join(dirs.RunMnt, "ubuntu-data/system-data/etc/cloud/")
	if err := os.MkdirAll(ubuntuDataCloud, 0755); err != nil {
		return fmt.Errorf("cannot make cloud config dir: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(ubuntuDataCloud, "cloud-init.disabled"), nil, 0644); err != nil {
		return fmt.Errorf("cannot disable cloud-init: %v", err)
	}

	return nil
}

// configureRunSystem configures the ubuntu-data partition with any
// configuration needed from e.g. the gadget or for cloud-init
func configureRunSystem(deviceCtx snapstate.DeviceContext) error {
	if err := configureCloudInit(deviceCtx); err != nil {
		return err
	}

	return nil
}
