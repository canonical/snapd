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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

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
	info, err := snapstate.GadgetInfo(st, deviceCtx)
	if err != nil {
		return fmt.Errorf("cannot get gadget info: %v", err)
	}
	gadgetDir := info.MountDir()

	// run the create partition code
	st.Unlock()
	output, err := exec.Command(filepath.Join(dirs.DistroLibExecDir, "snap-bootstrap"), "create-partitions", gadgetDir).CombinedOutput()
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot create partitions: %v", osutil.OutputErr(output, err))
	}

	// XXX: there must be a better way to get this information
	// we can only make a single recovery system bootable right now
	recoverySystems, err := filepath.Glob(filepath.Join(dirs.MountPointDir, "ubuntu-seed/systems/*"))
	if err != nil {
		return fmt.Errorf("cannot find recovery systems: %v", err)
	}
	if len(recoverySystems) > 1 {
		return fmt.Errorf("cannot make multiple recovery systems bootable yet")
	}
	logger.Noticef("recovery system is %s", recoverySystems[0])

	label := filepath.Base(recoverySystems[0])

	// make the partition bootable
	seed, err := seed.Open(filepath.Join(dirs.MountPointDir, "ubuntu-seed"), label)
	if err != nil {
		return fmt.Errorf("cannot open seed: %v", err)
	}
	if err := seed.LoadAssertions(nil, nil); err != nil {
		return fmt.Errorf("cannot load assertions: %v", err)
	}
	if err := seed.LoadMeta(perfTimings); err != nil {
		return fmt.Errorf("cannot load metadata: %v", err)
	}
	bootWith := &boot.BootableSet{
		RecoverySystemDir: recoverySystems[0],
		Recovery:          true,
	}
	for _, sn := range seed.EssentialSnaps() {
		snapf, err := snap.Open(sn.Path)
		if err != nil {
			return fmt.Errorf("cannot open snap info: %v", err)
		}
		info, err := snap.ReadInfoFromSnapFile(snapf, nil)
		if err != nil {
			return fmt.Errorf("cannot read snap info: %v", err)
		}
		switch info.GetType() {
		case snap.TypeOS, snap.TypeBase:
			bootWith.Base = info
			bootWith.BasePath = sn.Path
		case snap.TypeKernel:
			bootWith.Kernel = info
			bootWith.KernelPath = sn.Path
		}
	}

	logger.Noticef("seed base: %s", bootWith.BasePath)
	logger.Noticef("seed kernel: %s", bootWith.KernelPath)

	model, err := seed.Model()
	if err != nil {
		return fmt.Errorf("cannot get seed model: %v", err)
	}
	bootRootDir := filepath.Join(dirs.MountPointDir, "ubuntu-boot")
	if err := boot.MakeBootable(model, bootRootDir, bootWith); err != nil {
		return fmt.Errorf("cannot make system bootable: %v", err)
	}

	return nil
}
