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
func ApplyInstallerVolumesToGadget(installerVols map[string]*Volume, gadgetVols map[string]*Volume) error {
	for volName, gv := range gadgetVols {
		if len(gv.Partial) == 0 {
			continue
		}

		insVol := installerVols[volName]
		if insVol == nil {
			return fmt.Errorf("installer did not provide information for volume %q", volName)
		}

		// TODO: partial structure, as it is not clear what will be possible when set

		if gv.HasPartial(PartialSchema) {
			if insVol.Schema == "" {
				return fmt.Errorf("installer did not provide schema for volume %q", volName)
			}
			gv.Schema = insVol.Schema
		}

		if gv.HasPartial(PartialFilesystem) {
			if err := applyPartialFilesystem(insVol, gv, volName); err != nil {
				return err
			}
		}

		if gv.HasPartial(PartialSize) {
			if err := applyPartialSize(insVol, gv, volName); err != nil {
				return err
			}
		}

		// The only thing that can still be partial is the structure
		if gv.HasPartial(PartialStructure) {
			gv.Partial = []PartialProperty{PartialStructure}
		} else {
			gv.Partial = []PartialProperty{}
		}

		// Now validate finalized volume
		if err := validateVolume(gv); err != nil {
			return fmt.Errorf("finalized volume %q is wrong: %v", gv.Name, err)
		}
	}

	return nil
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
