// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

type coreBootParticipant struct {
	s  snap.PlaceInfo
	bs bootState
}

// ensure coreBootParticipant is a BootParticipant
var _ BootParticipant = (*coreBootParticipant)(nil)

func (*coreBootParticipant) IsTrivial() bool { return false }

func (bp *coreBootParticipant) SetNextBoot(bootCtx NextBootContext) (rebootInfo RebootInfo, err error) {
	modeenvLock()
	defer modeenvUnlock()

	const errPrefix = "cannot set next boot: %s"

	rebootInfo, u, err := bp.bs.setNext(bp.s, bootCtx)
	if err != nil {
		return RebootInfo{RebootRequired: false}, fmt.Errorf(errPrefix, err)
	}

	if u != nil {
		if err := u.commit(); err != nil {
			return RebootInfo{RebootRequired: false}, fmt.Errorf(errPrefix, err)
		}
	}

	return rebootInfo, nil
}

type coreKernel struct {
	s     snap.PlaceInfo
	bopts *bootloader.Options
}

// ensure coreKernel is a Kernel
var _ BootKernel = (*coreKernel)(nil)

func (*coreKernel) IsTrivial() bool { return false }

func (k *coreKernel) RemoveKernelAssets() error {
	// XXX: shouldn't we check the snap type?
	bootloader, err := bootloader.Find("", k.bopts)
	if err != nil {
		return fmt.Errorf("cannot remove kernel assets: %s", err)
	}

	// ask bootloader to remove the kernel assets if needed
	return bootloader.RemoveKernelAssets(k.s)
}

func (k *coreKernel) ExtractKernelAssets(snapf snap.Container) error {
	bootloader, err := bootloader.Find("", k.bopts)
	if err != nil {
		return fmt.Errorf("cannot extract kernel assets: %s", err)
	}
	// ask bootloader to extract the kernel assets if needed
	return bootloader.ExtractKernelAssets(k.s, snapf)
}
