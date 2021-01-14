// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// ValidationConstraints carries extra constraints on top of those
// implied by the model to use for gadget validation.
// They might be constraints that are determined only at runtime.
type ValidationConstraints struct {
	// EncryptedData when true indicates that the gadget will be used on a
	// device where the data partition will be encrypted.
	EncryptedData bool
}

// Validate checks that the given gadget metadata matches the
// consistency rules for role usage, labels etc as implied by the
// model and extra constraints that might be known only at runtime.
func Validate(info *Info, model Model, extra *ValidationConstraints) error {
	if err := ruleValidateVolumes(info.Volumes, model); err != nil {
		return err
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
		// TODO:UC20: shall we make sure that size of ubuntu-save is reasonable?
	}
	return nil
}

type roleInstance struct {
	volName string
	s       *VolumeStructure
}

func ruleValidateVolumes(vols map[string]*Volume, model Model) error {
	roles := map[string]*roleInstance{
		SystemSeed: nil,
		SystemBoot: nil,
		SystemData: nil,
		SystemSave: nil,
	}

	xvols := ""
	if len(vols) != 1 {
		xvols = " across volumes"
	}

	// TODO: is this too strict for old gadgets?
	for name, v := range vols {
		for i := range v.Structure {
			s := &v.Structure[i]
			if inst, ok := roles[s.Role]; ok {
				if inst != nil {
					return fmt.Errorf("cannot have more than one partition with %s role%s", s.Role, xvols)
				}
				roles[s.Role] = &roleInstance{
					volName: name,
					s:       s,
				}
			}
		}
	}

	expectedSeed := false
	if model != nil {
		expectedSeed = wantsSystemSeed(model)
	} else {
		// if system-seed role is mentioned assume the uc20
		// consistency rules
		expectedSeed = roles[SystemSeed] != nil
	}

	for name, v := range vols {
		if err := ruleValidateVolume(name, v, expectedSeed); err != nil {
			return fmt.Errorf("invalid volume %q: %v", name, err)
		}
	}

	if err := ensureRolesConsistency(roles, expectedSeed); err != nil {
		return err
	}

	return nil
}

func ruleValidateVolume(name string, vol *Volume, expectedSeed bool) error {
	for idx, s := range vol.Structure {
		if err := ruleValidateVolumeStructure(&s, expectedSeed); err != nil {
			return fmt.Errorf("invalid structure %v: %v", fmtIndexAndName(idx, s.Name), err)
		}
	}

	return nil
}

func ruleValidateVolumeStructure(vs *VolumeStructure, expectedSeed bool) error {
	var reservedLabels []string
	if expectedSeed {
		reservedLabels = reservedLabelsWithSeed
	} else {
		reservedLabels = reservedLabelsWithoutSeed
	}
	if err := validateReservedLabels(vs, reservedLabels); err != nil {
		return err
	}
	return nil
}

var (
	reservedLabelsWithSeed = []string{
		ubuntuBootLabel,
		ubuntuSeedLabel,
		ubuntuDataLabel,
		ubuntuSaveLabel,
	}

	// labels that we don't expect to be used on a UC16/18 system:
	//  * seed needs to be the ESP so there's a conflict
	//  * ubuntu-data is the main data partition which on UC16/18
	//    is expected to be named writable instead
	reservedLabelsWithoutSeed = []string{
		ubuntuSeedLabel,
		ubuntuDataLabel,
	}
)

func validateReservedLabels(vs *VolumeStructure, reservedLabels []string) error {
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

func ensureRolesConsistency(roles map[string]*roleInstance, expectedSeed bool) error {
	// TODO: should we validate usage of uc20 specific system-recovery-{image,select}
	//       roles too? they should only be used on uc20 systems, so models that
	//       have a grade set and are not classic

	switch {
	case roles[SystemSeed] == nil && roles[SystemData] == nil:
		if expectedSeed {
			return fmt.Errorf("model requires system-seed partition, but no system-seed or system-data partition found")
		}
	case roles[SystemSeed] != nil && roles[SystemData] == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case roles[SystemSeed] == nil && roles[SystemData] != nil:
		// error if we have the SystemSeed constraint but no actual system-seed structure
		if expectedSeed {
			return fmt.Errorf("model requires system-seed structure, but none was found")
		}
		// without SystemSeed, system-data label must be implicit or writable
		if err := checkImplicitLabel(SystemData, roles[SystemData].s, implicitSystemDataLabel); err != nil {
			return err
		}
	case roles[SystemSeed] != nil && roles[SystemData] != nil:
		// error if we don't have the SystemSeed constraint but we have a system-seed structure
		if !expectedSeed {
			return fmt.Errorf("model does not support the system-seed role")
		}
		if err := checkSeedDataImplicitLabels(roles); err != nil {
			return err
		}
	}
	if roles[SystemSave] != nil {
		if !expectedSeed {
			return fmt.Errorf("model does not support the system-save role")
		}
		if err := ensureSystemSaveRuleConsistency(roles); err != nil {
			return err
		}
	}

	if expectedSeed {
		// make sure that all roles come from the same volume
		// TODO:UC20: there is more to do in order to support multi-volume situations

		// if SystemSeed is unset we must have failed earlier
		seedVolName := roles[SystemSeed].volName

		for _, otherRole := range []string{SystemBoot, SystemData, SystemSave} {
			ri := roles[otherRole]
			if ri != nil && ri.volName != seedVolName {
				return fmt.Errorf("system-boot, system-data, and system-save are expected to share the same volume as system-seed")
			}
		}
	}

	return nil
}

func ensureSystemSaveRuleConsistency(roles map[string]*roleInstance) error {
	if roles[SystemData] == nil || roles[SystemSeed] == nil {
		// previous checks should stop reaching here
		return fmt.Errorf("internal error: system-save requires system-seed and system-data structures")
	}
	if err := checkImplicitLabel(SystemSave, roles[SystemSave].s, ubuntuSaveLabel); err != nil {
		return err
	}
	return nil
}

func checkSeedDataImplicitLabels(roles map[string]*roleInstance) error {
	if err := checkImplicitLabel(SystemData, roles[SystemData].s, ubuntuDataLabel); err != nil {
		return err
	}
	if err := checkImplicitLabel(SystemSeed, roles[SystemSeed].s, ubuntuSeedLabel); err != nil {
		return err
	}
	return nil
}

func checkImplicitLabel(role string, vs *VolumeStructure, implicitLabel string) error {
	if vs.Label != "" && vs.Label != implicitLabel {
		return fmt.Errorf("%s structure must have an implicit label or %q, not %q", role, implicitLabel, vs.Label)

	}
	return nil
}

// content validation

func splitKernelRef(kernelRef string) (asset, content string, err error) {
	// kernel ref has format: $kernel:<asset-name>/<content-path> where
	// asset name and content is listed in kernel.yaml, content looks like a
	// sane path
	if !strings.HasPrefix(kernelRef, "$kernel:") {
		return "", "", fmt.Errorf("internal error: splitKernelRef called for non kernel ref %q", kernelRef)
	}
	assetAndContent := kernelRef[len("$kernel:"):]
	l := strings.SplitN(assetAndContent, "/", 2)
	if len(l) < 2 {
		return "", "", fmt.Errorf("invalid asset and content in kernel ref %q", kernelRef)
	}
	asset = l[0]
	content = l[1]
	nonDirContent := content
	if strings.HasSuffix(nonDirContent, "/") {
		// a single trailing / is allowed to indicate all content under directory
		nonDirContent = strings.TrimSuffix(nonDirContent, "/")
	}
	if len(asset) == 0 || len(content) == 0 {
		return "", "", fmt.Errorf("missing asset name or content in kernel ref %q", kernelRef)
	}
	if filepath.Clean(nonDirContent) != nonDirContent || strings.Contains(content, "..") || nonDirContent == "/" {
		return "", "", fmt.Errorf("invalid content in kernel ref %q", kernelRef)
	}
	if !kernel.ValidAssetName.MatchString(asset) {
		return "", "", fmt.Errorf("invalid asset name in kernel ref %q", kernelRef)
	}
	return asset, content, nil
}

func validateVolumeContentsPresence(gadgetSnapRootDir string, vol *LaidOutVolume) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for _, s := range vol.LaidOutStructure {
		if !s.HasFilesystem() {
			continue
		}
		for _, c := range s.Content {
			// TODO: detect and skip Content with "$kernel:" style
			// refs if there is no kernelSnapRootDir passed in as
			// well
			if strings.HasPrefix(c.UnresolvedSource, "$kernel:") {
				// This only validates that the ref is valid.
				// Resolving happens with ResolveContentPaths()
				if _, _, err := splitKernelRef(c.UnresolvedSource); err != nil {
					return fmt.Errorf("cannot use kernel reference %q: %v", c.UnresolvedSource, err)
				}
				continue
			}
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

// ValidateContent checks whether the given directory contains valid matching content with respect to the given pre-validated gadget metadata.
func ValidateContent(info *Info, gadgetSnapRootDir string) error {
	// TODO: also validate that only one "<bl-name>.conf" file is
	// in the root directory of the gadget snap, because the
	// "<bl-name>.conf" file indicates precisely which bootloader
	// the gadget uses and as such there cannot be more than one
	// such bootloader
	for name, vol := range info.Volumes {
		lv, err := LayoutVolume(gadgetSnapRootDir, vol, defaultConstraints)
		if err != nil {
			return fmt.Errorf("invalid layout of volume %q: %v", name, err)
		}
		if err := validateVolumeContentsPresence(gadgetSnapRootDir, lv); err != nil {
			return fmt.Errorf("invalid volume %q: %v", name, err)
		}
	}
	return nil
}
