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

package gadget

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type validationState struct {
	SystemSeed *VolumeStructure
	SystemData *VolumeStructure
	SystemBoot *VolumeStructure
	SystemSave *VolumeStructure
}

func ruleValidateVolumes(vols map[string]*Volume, model Model) error {
	for name, v := range vols {
		if err := ruleValidateVolume(name, v, model); err != nil {
			return fmt.Errorf("invalid volume %q: %v", name, err)
		}
	}
	return nil
}

func ruleValidateVolume(name string, vol *Volume, model Model) error {
	state := &validationState{}

	for idx, s := range vol.Structure {
		if err := ruleValidateVolumeStructure(&s); err != nil {
			return fmt.Errorf("invalid structure %v: %v", fmtIndexAndName(idx, s.Name), err)
		}

		// XXX what about implicit roles?
		switch s.Role {
		case SystemSeed:
			if state.SystemSeed != nil {
				return fmt.Errorf("cannot have more than one partition with system-seed role")
			}
			state.SystemSeed = &vol.Structure[idx]
		case SystemData:
			if state.SystemData != nil {
				return fmt.Errorf("cannot have more than one partition with system-data role")
			}
			state.SystemData = &vol.Structure[idx]
		case SystemBoot:
			if state.SystemBoot != nil {
				return fmt.Errorf("cannot have more than one partition with system-boot role")
			}
			state.SystemBoot = &vol.Structure[idx]
		case SystemSave:
			if state.SystemSave != nil {
				return fmt.Errorf("cannot have more than one partition with system-save role")
			}
			state.SystemSave = &vol.Structure[idx]
		}

	}

	if err := ensureVolumeRuleConsistency(state, model); err != nil {
		return err
	}

	return nil
}

func ruleValidateVolumeStructure(vs *VolumeStructure) error {
	if err := validateReservedLabels(vs); err != nil {
		return err
	}
	return nil
}

var (
	reservedLabels = []string{
		// 2020-12-02 disabled because of customer gadget hotfix
		/*ubuntuBootLabel,*/
		ubuntuSeedLabel,
		ubuntuDataLabel,
		ubuntuSaveLabel,
	}
)

func validateReservedLabels(vs *VolumeStructure) error {
	if vs.Role != "" {
		// structure specifies a role, its labels will be checked later
		return nil
	}
	if vs.Label == "" {
		return nil
	}
	if strutil.ListContains(reservedLabels, vs.Label) {
		// a structure without a role uses one of reserved labels
		return fmt.Errorf("label %q is reserved", vs.Label)
	}
	return nil
}

func ensureVolumeRuleConsistencyNoConstraints(state *validationState) error {
	switch {
	case state.SystemSeed == nil && state.SystemData == nil:
		// happy so far
	case state.SystemSeed != nil && state.SystemData == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case state.SystemSeed == nil && state.SystemData != nil:
		if state.SystemData.Label != "" && state.SystemData.Label != implicitSystemDataLabel {
			return fmt.Errorf("system-data structure must have an implicit label or %q, not %q", implicitSystemDataLabel, state.SystemData.Label)
		}
	case state.SystemSeed != nil && state.SystemData != nil:
		if err := ensureSeedDataLabelsUnset(state); err != nil {
			return err
		}
	}
	if state.SystemSave != nil {
		if err := ensureSystemSaveRuleConsistency(state); err != nil {
			return err
		}
	}
	return nil
}

func ensureVolumeRuleConsistencyWithConstraints(state *validationState, model Model) error {
	// TODO: should we validate usage of uc20 specific system-recovery-{image,select}
	//       roles too? they should only be used on uc20 systems, so models that
	//       have a grade set and are not classic

	switch {
	case state.SystemSeed == nil && state.SystemData == nil:
		if wantsSystemSeed(model) {
			return fmt.Errorf("model requires system-seed partition, but no system-seed or system-data partition found")
		}
	case state.SystemSeed != nil && state.SystemData == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case state.SystemSeed == nil && state.SystemData != nil:
		// error if we have the SystemSeed constraint but no actual system-seed structure
		if wantsSystemSeed(model) {
			return fmt.Errorf("model requires system-seed structure, but none was found")
		}
		// without SystemSeed, system-data label must be implicit or writable
		if state.SystemData.Label != "" && state.SystemData.Label != implicitSystemDataLabel {
			return fmt.Errorf("system-data structure must have an implicit label or %q, not %q",
				implicitSystemDataLabel, state.SystemData.Label)
		}
	case state.SystemSeed != nil && state.SystemData != nil:
		// error if we don't have the SystemSeed constraint but we have a system-seed structure
		if !wantsSystemSeed(model) {
			return fmt.Errorf("model does not support the system-seed role")
		}
		if err := ensureSeedDataLabelsUnset(state); err != nil {
			return err
		}
	}
	if state.SystemSave != nil {
		if err := ensureSystemSaveRuleConsistency(state); err != nil {
			return err
		}
	}
	return nil
}

func ensureVolumeRuleConsistency(state *validationState, model Model) error {
	if model == nil {
		return ensureVolumeRuleConsistencyNoConstraints(state)
	}
	return ensureVolumeRuleConsistencyWithConstraints(state, model)
}

func ensureSeedDataLabelsUnset(state *validationState) error {
	if state.SystemData.Label != "" {
		return fmt.Errorf("system-data structure must not have a label")
	}
	if state.SystemSeed.Label != "" {
		return fmt.Errorf("system-seed structure must not have a label")
	}
	return nil
}

func ensureSystemSaveRuleConsistency(state *validationState) error {
	if state.SystemData == nil || state.SystemSeed == nil {
		return fmt.Errorf("system-save requires system-seed and system-data structures")
	}
	if state.SystemSave.Label != "" {
		return fmt.Errorf("system-save structure must not have a label")
	}
	return nil
}

// content validation

func validateVolumeContentsPresence(gadgetSnapRootDir string, vol *LaidOutVolume) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for _, s := range vol.LaidOutStructure {
		if !s.HasFilesystem() {
			continue
		}
		for _, c := range s.Content {
			// TODO: detect and skip Content with "$kernel:" style refs if there is no kernelSnapRootDir passed in as well
			realSource := filepath.Join(gadgetSnapRootDir, c.UnresolvedSource)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("structure %v, content %v: source path does not exist", s, c)
			}
			if strings.HasSuffix(c.ResolvedSource(), "/") {
				// expecting a directory
				if err := checkSourceIsDir(realSource + "/"); err != nil {
					return fmt.Errorf("structure %v, content %v: %v", s, c, err)
				}
			}
		}
	}
	return nil
}

func validateEncryptionSupport(info *Info) error {
	for name, vol := range info.Volumes {
		var haveSave bool
		for _, s := range vol.Structure {
			if s.Role == SystemSave {
				haveSave = true
			}
		}
		if !haveSave {
			return fmt.Errorf("volume %q has no structure with system-save role", name)
		}
		// XXX: shall we make sure that size of ubuntu-save is reasonable?
	}
	return nil
}

type ValidationConstraints struct {
	// EncryptedData when true indicates that the gadget will be used on a
	// device where the data partition will be encrypted.
	EncryptedData bool
}

// Validate checks whether the given directory contains valid gadget snap
// metadata and a matching content, under the provided model constraints, which
// are handled identically to ReadInfo(). Optionally takes additional validation
// constraints, which for instance may only be known at run time,
func Validate(gadgetSnapRootDir string, model Model, extra *ValidationConstraints) error {
	// TODO: also validate that only one "<bl-name>.conf" file is in the root
	//       directory  of the gadget snap, because the "<bl-name>.conf" file
	//       indicates precisely which bootloader the gadget uses and as such
	//       there cannot be more than one such bootloader
	info, err := ReadInfo(gadgetSnapRootDir, model)
	if err != nil {
		return fmt.Errorf("invalid gadget metadata: %v", err)
	}

	for name, vol := range info.Volumes {
		lv, err := LayoutVolume(gadgetSnapRootDir, vol, defaultConstraints)
		if err != nil {
			return fmt.Errorf("invalid layout of volume %q: %v", name, err)
		}
		if err := validateVolumeContentsPresence(gadgetSnapRootDir, lv); err != nil {
			return fmt.Errorf("invalid volume %q: %v", name, err)
		}
	}
	if extra != nil {
		if extra.EncryptedData {
			if err := validateEncryptionSupport(info); err != nil {
				return fmt.Errorf("gadget does not support encrypted data: %v", err)
			}
		}
	}
	return nil
}
