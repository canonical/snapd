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
	return bootloader.SetNextBoot(bs.s, bs.t)
}

func (bs *coreBootParticipant) ChangeRequiresReboot() bool {
	reqd, err := bootloader.ChangeRequiresReboot(bs.s, bs.t)
	if err != nil {
		logger.Noticef("%s", err)
	}
	return reqd
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
