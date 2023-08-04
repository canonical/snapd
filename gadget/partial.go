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
// by the installer and applies it to the gadget volumes for the
// device to install to and for properties partially defined,
// returning the result in a new Volume map. After that it checks that
// the gadget is now fully specified.
func ApplyInstallerVolumesToGadget(installerVols map[string]*Volume, gadgetVols map[string]*Volume) (map[string]*Volume, error) {
	newVols := map[string]*Volume{}
	for volName, gv := range gadgetVols {
		newV := gv.Copy()
		newVols[volName] = newV

		insVol := installerVols[volName]
		if insVol == nil {
			return nil, fmt.Errorf("installer did not provide information for volume %q", volName)
		}

		// First, retrieve device specified by installer
		for i := range newV.Structure {
			insStr, err := structureByName(insVol.Structure, newV.Structure[i].Name)
			if err != nil {
				return nil, err
			}
			newV.Structure[i].Device = insStr.Device
		}

		// Next changes are only for partial gadgets
		if len(newV.Partial) == 0 {
			continue
		}

		// TODO: partial structure, as it is not clear what will be possible when set

		if newV.HasPartial(PartialSchema) {
			if insVol.Schema == "" {
				return nil, fmt.Errorf("installer did not provide schema for volume %q", volName)
			}
			newV.Schema = insVol.Schema
		}

		if newV.HasPartial(PartialFilesystem) {
			if err := applyPartialFilesystem(insVol, newV, volName); err != nil {
				return nil, err
			}
		}

		if newV.HasPartial(PartialSize) {
			if err := applyPartialSize(insVol, newV, volName); err != nil {
				return nil, err
			}
		}

		// The only thing that can still be partial is the structure
		if newV.HasPartial(PartialStructure) {
			newV.Partial = []PartialProperty{PartialStructure}
		} else {
			newV.Partial = []PartialProperty{}
		}

		// Now validate finalized volume
		if err := validateVolume(newV); err != nil {
			return nil, fmt.Errorf("finalized volume %q is wrong: %v", newV.Name, err)
		}
	}

	return newVols, nil
}

func applyPartialFilesystem(insVol *Volume, gadgetVol *Volume, volName string) error {
	for sidx := range gadgetVol.Structure {
		vs := &gadgetVol.Structure[sidx]
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
	}
	return nil
}

func applyPartialSize(insVol *Volume, gadgetVol *Volume, volName string) error {
	for sidx := range gadgetVol.Structure {
		vs := &gadgetVol.Structure[sidx]
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
		vs.Offset = insStr.Offset
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
