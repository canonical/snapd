// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package gadget

import "fmt"

// ApplyInstallerVolumesToGadget takes the volume information returned
// by the installer and applies it to the laid out volumes for
// properties partially defined. After that it checks that the gadget
// is now fully specified.
func ApplyInstallerVolumesToGadget(installerVols map[string]*Volume, lovs map[string]*LaidOutVolume) error {
	for volName, lov := range lovs {
		if len(lov.Partial) == 0 {
			continue
		}

		insVol := installerVols[volName]
		if insVol == nil {
			return fmt.Errorf("installer did not provide information for volume %q", volName)
		}

		// TODO: partial structure, as it is not clear what will be possible when set

		if lov.HasPartial(PartialSchema) {
			if insVol.Schema == "" {
				return fmt.Errorf("installer did not provide schema for volume %q", volName)
			}
			lov.Schema = insVol.Schema
		}

		if lov.HasPartial(PartialFilesystem) {
			if err := applyPartialFilesystem(insVol, lov, volName); err != nil {
				return err
			}
		}

		if lov.HasPartial(PartialSize) {
			if err := applyPartialSize(insVol, lov, volName); err != nil {
				return err
			}
		}

		// The only thing that can still be partial is the structure
		if lov.HasPartial(PartialStructure) {
			lov.Partial = []PartialProperty{PartialStructure}
		} else {
			lov.Partial = []PartialProperty{}
		}

		// Now validate finalized volume
		if err := validateVolume(lov.Volume); err != nil {
			return fmt.Errorf("finalized volume %q is wrong: %v", lov.Name, err)
		}
	}

	return nil
}

func applyPartialFilesystem(insVol *Volume, lov *LaidOutVolume, volName string) error {
	for sidx := range lov.Structure {
		// Two structures to modify due to copies inside LaidOutVolume
		vs := &lov.Structure[sidx]
		vsLos := lov.LaidOutStructure[sidx].VolumeStructure
		if vs.Filesystem != "" || !vs.HasFilesystem() {
			continue
		}

		insStr, err := structureByName(insVol.Structure, vs.Name)
		if err != nil {
			return err
		}
		if insStr.Filesystem == "" {
			return fmt.Errorf("installer did not provide filesystem for structure %q in volume %q", vs.Name, volName)
		}

		vs.Filesystem = insStr.Filesystem
		vsLos.Filesystem = insStr.Filesystem
	}
	return nil
}

func applyPartialSize(insVol *Volume, lov *LaidOutVolume, volName string) error {
	for sidx := range lov.Structure {
		// Two structures to modify due to copies inside LaidOutVolume
		vs := &lov.Structure[sidx]
		vsLos := lov.LaidOutStructure[sidx].VolumeStructure
		if !vs.hasPartialSize() {
			continue
		}

		insStr, err := structureByName(insVol.Structure, vs.Name)
		if err != nil {
			return err
		}
		if insStr.Size == 0 {
			return fmt.Errorf("installer did not provide size for structure %q in volume %q", vs.Name, volName)
		}
		if insStr.Offset == nil {
			return fmt.Errorf("installer did not provide offset for structure %q in volume %q", vs.Name, volName)
		}

		vs.Size = insStr.Size
		vsLos.Size = insStr.Size
		vs.Offset = insStr.Offset
		vsLos.Offset = insStr.Offset
		lov.LaidOutStructure[sidx].StartOffset = *insStr.Offset
	}
	return nil
}

func structureByName(vss []VolumeStructure, name string) (*VolumeStructure, error) {
	for sidx := range vss {
		if vss[sidx].Name == name {
			return &vss[sidx], nil
		}
	}
	return nil, fmt.Errorf("cannot find structure %q", name)
}
