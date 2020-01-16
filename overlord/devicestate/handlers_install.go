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

	// run the create partition code
	st.Unlock()
	output, err := exec.Command(filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "create-partitions", "--mount", gadgetDir).CombinedOutput()
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot create partitions: %v", osutil.OutputErr(output, err))
	}

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
