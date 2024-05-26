// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2021 Canonical Ltd
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
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(ruleValidateVolumes(info.Volumes, model, extra))

	return nil
}

type roleInstance struct {
	volName string
	s       *VolumeStructure
}

func ruleValidateVolumes(vols map[string]*Volume, model Model, extra *ValidationConstraints) error {
	roles := map[string]*roleInstance{
		SystemSeed:     nil,
		SystemSeedNull: nil,
		SystemBoot:     nil,
		SystemData:     nil,
		SystemSave:     nil,
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
	if roles[SystemSeed] != nil && roles[SystemSeedNull] != nil {
		return fmt.Errorf("cannot have more than one partition with %s/%s role%s", SystemSeed, SystemSeedNull, xvols)
	}

	hasModes := false
	// Classic with gadget + kernel snaps
	// TODO: do the rules need changing now that we allow to omit
	// gadget and kernel?
	isClassicWithModes := false
	if model != nil {
		hasModes = hasGrade(model)
		isClassicWithModes = classicOrUndetermined(model) && hasModes
	} else {
		// if system-seed role is mentioned assume the uc20
		// consistency rules
		hasModes = roles[SystemSeed] != nil
		// if system-seed-null, this is classic with modes
		// Note that this will not be true if we use this role in
		// the future for UC cloud images.
		if roles[SystemSeedNull] != nil {
			hasModes = true
			isClassicWithModes = true
		}
	}

	for name, v := range vols {
		mylog.Check(ruleValidateVolume(v, hasModes))
	}

	if isClassicWithModes {
		mylog.Check(ensureRolesConsistencyClassicWithModes(roles))
	} else {
		mylog.Check(
			// The seed is expected on UC with modes
			ensureRolesConsistency(roles, hasModes))
	}

	if extra != nil {
		if extra.EncryptedData {
			if !hasModes {
				return fmt.Errorf("internal error: cannot support encrypted data in a system without modes")
			}
			if roles[SystemSave] == nil {
				return fmt.Errorf("gadget does not support encrypted data: required partition with system-save role is missing")
				// TODO:UC20: shall we make sure that size of ubuntu-save is reasonable?
			}
		}
	}

	return nil
}

func ruleValidateVolume(vol *Volume, hasModes bool) error {
	for idx, s := range vol.Structure {
		mylog.Check(ruleValidateVolumeStructure(&s, hasModes))
	}

	return nil
}

func ruleValidateVolumeStructure(vs *VolumeStructure, hasModes bool) error {
	var reservedLabels []string
	if hasModes {
		reservedLabels = reservedLabelsWithModes
	} else {
		reservedLabels = reservedLabelsWithoutModes
	}
	mylog.Check(validateReservedLabels(vs, reservedLabels))

	return nil
}

var (
	reservedLabelsWithModes = []string{
		ubuntuBootLabel,
		ubuntuSeedLabel,
		ubuntuDataLabel,
		ubuntuSaveLabel,
	}

	// labels that we don't expect to be used on a UC16/18 system:
	//  * seed needs to be the ESP so there's a conflict
	//  * ubuntu-data is the main data partition which on UC16/18
	//    is expected to be named writable instead
	reservedLabelsWithoutModes = []string{
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
	for _, reservedLabel := range reservedLabels {
		if vs.HasLabel(reservedLabel) {
			return fmt.Errorf("label %q is reserved", vs.Label)
		}
	}
	return nil
}

func ensureRolesConsistency(roles map[string]*roleInstance, seedExpected bool) error {
	// TODO: should we validate usage of uc20 specific system-recovery-{image,select}
	//       roles too? they should only be used on uc20 systems, so models that
	//       have a grade set and are not classic boot

	switch {
	case roles[SystemSeed] == nil && roles[SystemData] == nil:
		if seedExpected {
			return fmt.Errorf("model requires system-seed partition, but no system-seed or system-data partition found")
		}
	case roles[SystemSeed] != nil && roles[SystemData] == nil:
		return fmt.Errorf("the system-seed role requires system-data to be defined")
	case roles[SystemSeed] == nil && roles[SystemData] != nil:
		// error if we have the SystemSeed constraint but no actual system-seed structure
		if seedExpected {
			return fmt.Errorf("model requires system-seed structure, but none was found")
		}
		mylog.Check(
			// without SystemSeed, system-data label must be implicit or writable
			checkImplicitLabels(roles,
				roleLabel{role: SystemData, label: implicitSystemDataLabel}))

	case roles[SystemSeed] != nil && roles[SystemData] != nil:
		// error if we don't have the SystemSeed constraint but we have a system-seed structure
		if !seedExpected {
			return fmt.Errorf("model does not support the system-seed role")
		}
		mylog.Check(checkImplicitLabels(roles, roleLabelData, roleLabelSeed))

	}
	if roles[SystemSave] != nil {
		if !seedExpected {
			return fmt.Errorf("model does not support the system-save role")
		}
		mylog.Check(ensureSystemSaveRuleConsistency(roles))

	}

	if seedExpected {
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

func ensureRolesConsistencyClassicWithModes(roles map[string]*roleInstance) error {
	if roles[SystemBoot] == nil || roles[SystemData] == nil {
		return fmt.Errorf("system-boot and system-data roles are needed on classic")
	}

	// Make sure labels are as expected - save is optional
	roleLabelToCheck := []roleLabel{roleLabelBoot, roleLabelData}
	roleLabelOptional := []roleLabel{roleLabelSeed, roleLabelSeedNull, roleLabelSave}
	for _, rlLb := range roleLabelOptional {
		if roles[rlLb.role] != nil {
			roleLabelToCheck = append(roleLabelToCheck, rlLb)
		}
	}
	mylog.Check(checkImplicitLabels(roles, roleLabelToCheck...))

	// Check that boot/seed/seed-null/save are in the same volume
	bootVolName := roles[SystemBoot].volName
	for _, role := range []string{SystemSeed, SystemSeedNull, SystemSave} {
		if roles[role] != nil && roles[role].volName != bootVolName {
			return fmt.Errorf("system-boot and %s are expected to share the same volume", role)
		}
	}
	return nil
}

func ensureSystemSaveRuleConsistency(roles map[string]*roleInstance) error {
	if roles[SystemData] == nil || roles[SystemSeed] == nil {
		// previous checks should stop reaching here
		return fmt.Errorf("internal error: system-save requires system-seed and system-data structures")
	}
	mylog.Check(checkImplicitLabels(roles, roleLabelSave))

	return nil
}

// roleLabel contains a partition role and the default expected label.
type roleLabel struct {
	role  string
	label string
}

var (
	roleLabelSeed     = roleLabel{role: SystemSeed, label: ubuntuSeedLabel}
	roleLabelSeedNull = roleLabel{role: SystemSeedNull, label: ubuntuSeedLabel}
	roleLabelBoot     = roleLabel{role: SystemBoot, label: ubuntuBootLabel}
	roleLabelSave     = roleLabel{role: SystemSave, label: ubuntuSaveLabel}
	roleLabelData     = roleLabel{role: SystemData, label: ubuntuDataLabel}
)

func checkImplicitLabels(roles map[string]*roleInstance, roleLabels ...roleLabel) error {
	for _, rlLb := range roleLabels {
		volStruct := roles[rlLb.role].s
		if volStruct == nil {
			return fmt.Errorf("internal error: %q not in volume", rlLb.role)
		}
		if volStruct.Label != "" && !volStruct.HasLabel(rlLb.label) {
			return fmt.Errorf("%s structure must have an implicit label or %q, not %q", rlLb.role, rlLb.label, volStruct.Label)
		}
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

func validateVolumeContentsPresence(gadgetSnapRootDir string, vol *Volume) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for _, s := range vol.Structure {
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
				_, _ := mylog.Check3(splitKernelRef(c.UnresolvedSource))

				continue
			}
			realSource := filepath.Join(gadgetSnapRootDir, c.UnresolvedSource)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("structure #%d (%q), content %v: source path does not exist", s.YamlIndex, s.Name, c)
			}
			if strings.HasSuffix(c.UnresolvedSource, "/") {
				mylog.Check(
					// expecting a directory
					checkSourceIsDir(realSource + "/"))
			}
		}
	}
	return nil
}

// ValidateContent checks whether the given directory contains valid matching content with respect to the given pre-validated gadget metadata.
func ValidateContent(info *Info, gadgetSnapRootDir, kernelSnapRootDir string) error {
	// TODO: also validate that only one "<bl-name>.conf" file is
	// in the root directory of the gadget snap, because the
	// "<bl-name>.conf" file indicates precisely which bootloader
	// the gadget uses and as such there cannot be more than one
	// such bootloader
	var kernelInfo *kernel.Info
	if kernelSnapRootDir != "" {
		kernelInfo = mylog.Check2(kernel.ReadInfo(kernelSnapRootDir))
	}
	for name, vol := range info.Volumes {
		// Check that files shipped in the gadget have the expected sizes
		for idx := range vol.Structure {
			mylog.Check(checkGadgetContentImages(gadgetSnapRootDir, &vol.Structure[idx]))
		}
		// Make sure that content can be resolved if the kernel snap is known.
		if kernelInfo != nil {
			for idx := range vol.Structure {
				mylog.Check2(resolveVolumeContent(gadgetSnapRootDir, kernelSnapRootDir, kernelInfo, &vol.Structure[idx], nil))
			}
		}
		mylog.Check(validateVolumeContentsPresence(gadgetSnapRootDir, vol))

	}

	// Ensure that at least one kernel.yaml reference can be resolved
	// by the gadget
	if kernelInfo != nil {
		resolvedOnce := false
		for _, vol := range info.Volumes {
			mylog.Check(gadgetVolumeConsumesOneKernelUpdateAsset(vol, kernelInfo))
			if err == nil {
				resolvedOnce = true
			}
		}
		if !resolvedOnce {
			return fmt.Errorf("no asset from the kernel.yaml needing synced update is consumed by the gadget at %q", gadgetSnapRootDir)
		}
	}

	return nil
}

// gadgetVolumeConsumesOneKernelUpdateAsset ensures that at least one kernel
// assets from the kernel.yaml has a reference in the given
// LaidOutVolume.
func gadgetVolumeConsumesOneKernelUpdateAsset(pNew *Volume, kernelInfo *kernel.Info) error {
	notFoundAssets, _ := mylog.Check3(searchConsumedAssets(pNew, kernelInfo.Assets))

	if len(notFoundAssets) > 0 {
		sort.Strings(notFoundAssets)
		return fmt.Errorf("gadget does not consume any of the kernel assets needing synced update %s", strutil.Quoted(notFoundAssets))
	}
	return nil
}

func searchConsumedAssets(pNew *Volume, assets map[string]*kernel.Asset) (missingAssets []string, consumedAny bool, err error) {
	notFoundAssets := make([]string, 0, len(assets))
	for assetName, asset := range assets {
		if !asset.Update {
			continue
		}
		for _, ps := range pNew.Structure {
			for _, rc := range ps.Content {
				pathOrRef := rc.UnresolvedSource
				if !strings.HasPrefix(pathOrRef, "$kernel:") {
					// regular asset from the gadget snap
					continue
				}
				wantedAsset, _ := mylog.Check3(splitKernelRef(pathOrRef))

				if assetName == wantedAsset {
					// found a valid kernel asset,
					// that is enough
					return nil, true, nil
				}
			}
		}
		notFoundAssets = append(notFoundAssets, assetName)
	}

	return notFoundAssets, false, nil
}

// gadgetVolumeKernelUpdateAssetsConsumed ensures that at least one kernel
// assets from the kernel.yaml has a reference in the given
// LaidOutVolume.
func gadgetVolumeKernelUpdateAssetsConsumed(pNew *Volume, kernelInfo *kernel.Info) (bool, error) {
	_, consumedAny := mylog.Check3(searchConsumedAssets(pNew, kernelInfo.Assets))

	return consumedAny, nil
}
