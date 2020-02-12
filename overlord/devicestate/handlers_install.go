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

	"github.com/chrisccoulson/ubuntu-core-fde-utils"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

	kernelInfo, err := snapstate.KernelInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get kernel info: %v", err)
	}
	kernelDir := kernelInfo.MountDir()

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
		ubuntuDataDir := filepath.Join(dirs.RunMnt, "ubuntu-data", "system-data")

		// TODO:UC20: set final file names
		args = append(args,
			// enable data encryption
			"--encrypt",
			// location to store the sealed keyfile
			"--key-file", filepath.Join(ubuntuBootDir, "ubuntu-data.keyfile.sealed"),
			// location to store the recovery keyfile
			"--recovery-key-file", filepath.Join(ubuntuDataDir, "recovery.txt"),
			// location to store the lockout authorization data
			"--lockout-auth-file", filepath.Join(ubuntuDataDir, "lockout-auth"),
			// location to store the authorization policy update data
			"--auth-update-file", filepath.Join(ubuntuDataDir, "auth-update"),
			// path to the kernel to install
			"--kernel-path", filepath.Join(kernelDir, "kernel.efi"),
		)
	}
	args = append(args, gadgetDir)

	// run the create partition code
	logger.Noticef("create and deploy partitions")
	st.Unlock()
	output, err := exec.Command(filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), args...).CombinedOutput()
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot create partitions: %v", osutil.OutputErr(output, err))
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

	logger.Noticef("make system bootable")
	rootdir := dirs.GlobalRootDir
	if err := bootMakeBootable(deviceCtx.Model(), rootdir, bootWith); err != nil {
		return fmt.Errorf("cannot make run system bootable: %v", err)
	}

	// request a restart as the last action after a successful install
	logger.Noticef("request system restart")
	st.RequestRestart(state.RestartSystem)

	return nil
}

var checkTPMAvailability = func() error {
	logger.Noticef("checking TPM device availability...")
	// TODO:UC20: verify if gadget contains a TPM endorsement key certificate, and
	//            establish a secure connection to verify the device authenticity
	tconn, err := fdeutil.ConnectToDefaultTPM()
	if err != nil {
		logger.Noticef("connection to TPM device failed: %v", err)
		return err
	}
	logger.Noticef("TPM device detected")
	return tconn.Close()
}

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
