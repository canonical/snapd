// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package install

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
)

// KernelSnapInfo includes information from the kernel snap that is
// needed to build a drivers tree.
type KernelSnapInfo struct {
	Name     string
	Revision snap.Revision
	// MountPoint is the root of the files from the kernel snap
	MountPoint string
	// NeedsDriversTree will be set if a drivers tree needs to be
	// built on installation
	NeedsDriversTree bool
	// IsCore is set if this is UC
	IsCore bool
	// ModulesComps has the information for installed
	// kernel-modules components from the snap
	ModulesComps []KernelModulesComponentInfo
}

// KernelModulesComponentInfo includes information for kernel-modules
// components that is needed to build a drivers tree.
// TODO:COMPS support modules created by hooks in $SNAP_DATA.
type KernelModulesComponentInfo struct {
	Name     string
	Revision snap.Revision
	// MountPoint is the root of the files from the component
	MountPoint string
}

type CompSeedInfo struct {
	CompInfo *snap.ComponentInfo
	CompSeed *seed.Component
}

func KernelBootInfo(kernInfo *snap.Info, compSeedInfos []CompSeedInfo, kernMntPoint string, mntPtForComps map[string]string, isCore, needsDriversTree bool) (*KernelSnapInfo, []boot.BootableKModsComponents) {
	bootKMods := make([]boot.BootableKModsComponents, 0, len(compSeedInfos))
	modulesComps := make([]KernelModulesComponentInfo, 0, len(compSeedInfos))
	for _, compSeedInfo := range compSeedInfos {
		ci := compSeedInfo.CompInfo
		if ci.Type == snap.KernelModulesComponent {
			cpi := snap.MinimalComponentContainerPlaceInfo(ci.Component.ComponentName,
				ci.Revision, kernInfo.SnapName())
			modulesComps = append(modulesComps, KernelModulesComponentInfo{
				Name:       ci.Component.ComponentName,
				Revision:   ci.Revision,
				MountPoint: mntPtForComps[ci.FullName()],
			})
			bootKMods = append(bootKMods, boot.BootableKModsComponents{
				CompPlaceInfo: cpi,
				CompPath:      compSeedInfo.CompSeed.Path,
			})
		}
	}

	kSnapInfo := &KernelSnapInfo{
		Name:             kernInfo.SnapName(),
		Revision:         kernInfo.Revision,
		MountPoint:       kernMntPoint,
		IsCore:           isCore,
		ModulesComps:     modulesComps,
		NeedsDriversTree: needsDriversTree,
	}

	return kSnapInfo, bootKMods
}
