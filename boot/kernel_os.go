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
	"github.com/snapcore/snapd/snap"
)

type coreBootParticipant struct {
	s snap.PlaceInfo
	t snap.Type
}

// ensure coreBootParticipant is a BootParticipant
var _ BootParticipant = (*coreBootParticipant)(nil)

func (*coreBootParticipant) IsTrivial() bool { return false }

func (bs *coreBootParticipant) SetNextBoot() (rebootRequired bool, err error) {
	bootloader, err := bootloader.Find("", nil)
	if err != nil {
		return false, fmt.Errorf("cannot set next boot: %s", err)
	}

	var nextBootVar, goodBootVar string
	switch bs.t {
	case snap.TypeOS, snap.TypeBase:
		nextBootVar = "snap_try_core"
		goodBootVar = "snap_core"
	case snap.TypeKernel:
		nextBootVar = "snap_try_kernel"
		goodBootVar = "snap_kernel"
	}
	nextBoot := filepath.Base(bs.s.MountFile())

	// check if we actually need to do anything, i.e. the exact same
	// kernel/core revision got installed again (e.g. firstboot)
	// and we are not in any special boot mode
	m, err := bootloader.GetBootVars("snap_mode", goodBootVar)
	if err != nil {
		return false, fmt.Errorf("cannot set next boot: %s", err)
	}

	snapMode := "try"
	rebootRequired = true
	if m[goodBootVar] == nextBoot {
		// If we were in anything but default ("") mode before
		// and now switch to the good core/kernel again, make
		// sure to clean the snap_mode here. This also
		// mitigates https://forum.snapcraft.io/t/5253
		if m["snap_mode"] == "" {
			// already clean
			return false, nil
		}
		// clean
		snapMode = ""
		nextBoot = ""
		rebootRequired = false
	}

	if err := bootloader.SetBootVars(map[string]string{
		"snap_mode": snapMode,
		nextBootVar: nextBoot,
	}); err != nil {
		return false, err
	}

	return rebootRequired, nil
}

type coreKernel struct {
	s snap.PlaceInfo
}

// ensure coreKernel is a Kernel
var _ BootKernel = (*coreKernel)(nil)

func (*coreKernel) IsTrivial() bool { return false }

func (k *coreKernel) RemoveKernelAssets() error {
	// XXX: shouldn't we check the snap type?
	bootloader, err := bootloader.Find("", nil)
	if err != nil {
		return fmt.Errorf("cannot remove kernel assets: %s", err)
	}

	// ask bootloader to remove the kernel assets if needed
	return bootloader.RemoveKernelAssets(k.s)
}

func (k *coreKernel) ExtractKernelAssets(snapf snap.Container) error {
	bootloader, err := bootloader.Find("", nil)
	if err != nil {
		return fmt.Errorf("cannot extract kernel assets: %s", err)
	}

	// ask bootloader to extract the kernel assets if needed
	return bootloader.ExtractKernelAssets(k.s, snapf)
}

type bootState16 struct {
	varSuffix string
	errName   string
}

func newBootState16(typ snap.Type) *bootState16 {
	var varSuffix, errName string
	switch typ {
	case snap.TypeKernel:
		varSuffix = "kernel"
		errName = "kernel"
	case snap.TypeBase:
		varSuffix = "core"
		errName = "boot base"
	default:
		panic(fmt.Sprintf("cannot make a bootState16 for snap type %q", typ))
	}
	return &bootState16{varSuffix: varSuffix, errName: errName}
}

func (s *bootState16) revisions() (snap, try_snap *NameAndRevision, trying bool, err error) {
	bloader, err := bootloader.Find("", nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get boot settings: %s", err)
	}

	snapVar := "snap_" + s.varSuffix
	trySnapVar := "snap_try_" + s.varSuffix
	vars := []string{"snap_mode", snapVar, trySnapVar}
	snaps := make(map[string]*NameAndRevision, 2)

	m, err := bloader.GetBootVars(vars...)
	if err != nil {
		return nil, nil, false, fmt.Errorf("cannot get boot variables: %s", err)
	}

	for _, vName := range vars {
		v := m[vName]
		if v == "" && vName != snapVar {
			// snap_mode & snap_try_<type> can be empty
			// snap_<type> cannot be! and will fail parsing
			// below
			continue
		}

		if vName == "snap_mode" {
			trying = v == "trying"
		} else {
			nameAndRevno, err := nameAndRevnoFromSnap(v)
			if err != nil {
				return nil, nil, false, fmt.Errorf("cannot get name and revision of %s (%s): %v", s.errName, vName, err)
			}
			snaps[vName] = nameAndRevno
		}
	}

	return snaps[snapVar], snaps[trySnapVar], trying, nil
}
