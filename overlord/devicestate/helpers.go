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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func setDeviceFromModelAssertion(st *state.State, device *auth.DeviceState, model *asserts.Model) error {
	device.Brand = model.BrandID()
	device.Model = model.Model()
	return internal.SetDevice(st, device)
}

func gadgetDataFromInfo(info *snap.Info, constraints *gadget.ModelConstraints) (*gadget.GadgetData, error) {
	gi, err := gadget.ReadInfo(info.MountDir(), coreGadgetConstraints)
	if err != nil {
		return nil, err
	}
	return &gadget.GadgetData{Info: gi, RootDir: info.MountDir()}, nil
}

var (
	gadgetFindDeviceForStructure = gadget.FindDeviceForStructure
)

// partitionFromLabel returns the node of the block device used by the filesystem
// with the specified label.
func partitionFromLabel(gadgetDir, label string) (string, error) {
	structureFromLabel := func(laidOutStructure []gadget.LaidOutStructure, label string) *gadget.LaidOutStructure {
		for _, ps := range laidOutStructure {
			if ps.VolumeStructure.Label == label {
				return &ps
			}
		}
		return nil
	}

	pv, err := gadget.PositionedVolumeFromGadget(gadgetDir)
	if err != nil {
		return "", err
	}
	targetStructure := structureFromLabel(pv.LaidOutStructure, label)
	if targetStructure == nil {
		return "", fmt.Errorf("cannot find structure with label %q", label)
	}
	return gadgetFindDeviceForStructure(targetStructure)
}

var (
	sysClassBlock = "/sys/class/block"
	devBlock      = "/dev/block"
)

// diskFromPartition returns the node of the disk device that contains the
// specified partition. Note that this requires real disk partitions and won't
// work with device-mapped block devices.
func diskFromPartition(part string) (string, error) {
	sysdev := filepath.Join(sysClassBlock, filepath.Base(part))
	dev, err := filepath.EvalSymlinks(sysdev)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlink: %s: %s", sysdev, err)
	}

	devpath := filepath.Join(filepath.Dir(dev), "dev")
	f, err := os.Open(devpath)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %s", devpath, err)
	}
	defer f.Close()

	// Read major and minor block device numbers
	r := bufio.NewReader(f)
	line, _, err := r.ReadLine()
	nums := strings.TrimSpace(string(line))
	if err != nil {
		return "", fmt.Errorf("cannot read major and minor numbers: %s", err)
	}

	// Locate block device based on device numbers
	blockdev := filepath.Join(devBlock, nums)
	voldev, err := filepath.EvalSymlinks(blockdev)
	if err != nil {
		return "", fmt.Errorf("cannot resolve symlink: %s: %s", blockdev, err)
	}

	return voldev, nil
}
