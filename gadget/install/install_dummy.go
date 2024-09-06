// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot

/*
 * Copyright (C) 2019-2020, 2024 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/timings"
)

func Run(model gadget.Model, gadgetRoot string, kernelSnapInfo *KernelSnapInfo, bootDevice string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	return nil, fmt.Errorf("build without secboot support")
}

func FactoryReset(model gadget.Model, gadgetRoot string, kernelSnapInfo *KernelSnapInfo, bootDevice string, options Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*InstalledSystemSideData, error) {
	return nil, fmt.Errorf("build without secboot support")
}

func WriteContent(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *EncryptionSetupData, kSnapInfo *KernelSnapInfo, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error) {
	return nil, fmt.Errorf("build without secboot support")
}

func MountVolumes(onVolumes map[string]*gadget.Volume, encSetupData *EncryptionSetupData) (seedMntDir string, unmount func() error, err error) {
	return "", nil, fmt.Errorf("build without secboot support")
}

func SaveStorageTraits(model gadget.Model, vols map[string]*gadget.Volume, encryptSetupData *EncryptionSetupData) error {
	return fmt.Errorf("build without secboot support")
}

func EncryptPartitions(onVolumes map[string]*gadget.Volume, encryptionType secboot.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string,
	perfTimings timings.Measurer) (*EncryptionSetupData, error) {
	return nil, fmt.Errorf("build without secboot support")
}

func ResetterForRole(setupData *EncryptionSetupData) map[string]secboot.KeyResetter {
	return nil
}

func MatchDisksToGadgetVolumes(gVols map[string]*gadget.Volume,
	volCompatOpts *gadget.VolumeCompatibilityOptions) (map[string]map[int]*gadget.OnDiskStructure, error) {
	return nil, fmt.Errorf("build without secboot support")
}
