// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
)

type coreBootParticipant struct {
	s snap.PlaceInfo
	t snap.Type
}

// ensure coreBootParticipant is a BootParticipant
var _ BootParticipant = (*coreBootParticipant)(nil)

func (bs *coreBootParticipant) SetNextBoot() error {
	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}

	var nextBoot, goodBoot string
	switch bs.t {
	case snap.TypeOS, snap.TypeBase:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	}
	blobName := filepath.Base(bs.s.MountFile())

	// check if we actually need to do anything, i.e. the exact same
	// kernel/core revision got installed again (e.g. firstboot)
	// and we are not in any special boot mode
	m, err := bootloader.GetBootVars("snap_mode", goodBoot)
	if err != nil {
		return fmt.Errorf("cannot set next boot: %s", err)
	}
	if m[goodBoot] == blobName {
		// If we were in anything but default ("") mode before
		// and now switch to the good core/kernel again, make
		// sure to clean the snap_mode here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		if m["snap_mode"] != "" {
			return bootloader.SetBootVars(map[string]string{
				"snap_mode": "",
				nextBoot:    "",
			})
		}
		return nil
	}

	return bootloader.SetBootVars(map[string]string{
		nextBoot:    blobName,
		"snap_mode": "try",
	})
}

func (bs *coreBootParticipant) ChangeRequiresReboot() bool {
	bootloader, err := bootloader.Find()
	if err != nil {
		logger.Noticef("cannot get boot settings: %s", err)
		return false
	}

	var nextBoot, goodBoot string
	switch bs.t {
	case snap.TypeKernel:
		nextBoot = "snap_try_kernel"
		goodBoot = "snap_kernel"
	case snap.TypeOS, snap.TypeBase:
		nextBoot = "snap_try_core"
		goodBoot = "snap_core"
	}

	m, err := bootloader.GetBootVars(nextBoot, goodBoot)
	if err != nil {
		logger.Noticef("cannot get boot variables: %s", err)
		return false
	}

	squashfsName := filepath.Base(bs.s.MountFile())
	if m[nextBoot] == squashfsName && m[goodBoot] != m[nextBoot] {
		return true
	}

	return false
}

type coreKernel struct {
	*coreBootParticipant
}

// ensure coreKernel is a Kernel
var _ Kernel = (*coreKernel)(nil)

func (k *coreKernel) RemoveKernelAssets() error {
	// XXX: shouldn't we check the snap type?
	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot remove kernel assets: %s", err)
	}

	// ask bootloader to remove the kernel assets if needed
	return bootloader.RemoveKernelAssets(k.s)
}

func (k *coreKernel) ExtractKernelAssets(snapf snap.Container) error {
	bootloader, err := bootloader.Find()
	if err != nil {
		return fmt.Errorf("cannot extract kernel assets: %s", err)
	}

	// ask bootloader to extract the kernel assets if needed
	return bootloader.ExtractKernelAssets(k.s, snapf)
}
