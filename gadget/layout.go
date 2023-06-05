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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/secboot"
)

// LayoutOptions defines the options to layout a given volume.
type LayoutOptions struct {
	// SkipResolveContent will skip resolving content paths
	// and `$kernel:` style references
	SkipResolveContent bool

	// IgnoreContent will skip laying out content structure data to the
	// volume. Settings this implies "SkipResolveContent".  This
	// is used when only the partitions need to get
	// created and content gets written later.
	IgnoreContent bool

	GadgetRootDir string
	KernelRootDir string

	EncType secboot.EncryptionType
}

// NonMBRStartOffset is the minimum start offset of the first non-MBR structure
// in the volume that does not specify explicitly an offset. It can be ignored
// by setting explicitly offsets.
const NonMBRStartOffset = 1 * quantity.OffsetMiB

// LaidOutVolume defines the size of a volume and arrangement of all the
// structures within it
type LaidOutVolume struct {
	*Volume
	// LaidOutStructure is a list of structures within the volume, sorted
	// by their start offsets
	LaidOutStructure []LaidOutStructure
}

// PartiallyLaidOutVolume defines the layout of volume structures, but lacks the
// details about the layout of raw image content within the bare structures.
type PartiallyLaidOutVolume struct {
	*Volume
	// LaidOutStructure is a list of structures within the volume, sorted
	// by their start offsets
	LaidOutStructure []LaidOutStructure
}

// LaidOutStructure describes a VolumeStructure coming from the gadget plus the
// OnDiskStructure that describes how it would be applied to a given disk and
// additional content used when writing/updating data in the structure.
type LaidOutStructure struct {
	OnDiskStructure
	// VolumeStructure is the volume structure defined in gadget.yaml
	VolumeStructure *VolumeStructure
	// LaidOutContent is a list of raw content inside the structure
	LaidOutContent []LaidOutContent
	// ResolvedContent is a list of filesystem content that has all
	// relative paths or references resolved
	ResolvedContent []ResolvedContent
}

// These accessors return currently what comes in the gadget, but will use
// OnDiskVolume data when the latter is made part of LaidOutStructure.

// Type returns the type of the structure, which can be 2-hex digit MBR
// partition, 36-char GUID partition, comma separated <mbr>,<guid> for hybrid
// partitioning schemes, or 'bare' when the structure is not considered a
// partition.
//
// For backwards compatibility type 'mbr' can also be returned, and
// that is equivalent to role 'mbr'.
func (l LaidOutStructure) Type() string {
	return l.VolumeStructure.Type
}

// Name returns the partition label.
func (l LaidOutStructure) Name() string {
	return l.VolumeStructure.Name
}

// Label returns the filesystem label.
func (l LaidOutStructure) Label() string {
	return l.VolumeStructure.Label
}

// Filesystem for formatting the structure.
func (l LaidOutStructure) Filesystem() string {
	return l.VolumeStructure.Filesystem
}

// Role for the structure as specified in the gadget.
func (l LaidOutStructure) Role() string {
	return l.VolumeStructure.Role
}

// HasFilesystem returns true if the structure is using a filesystem.
func (l *LaidOutStructure) HasFilesystem() bool {
	return l.VolumeStructure.HasFilesystem()
}

// IsPartition returns true when the structure describes a partition in a block
// device.
func (l *LaidOutStructure) IsPartition() bool {
	return l.VolumeStructure.IsPartition()
}

func (p LaidOutStructure) String() string {
	return fmtIndexAndName(p.VolumeStructure.YamlIndex, p.Name())
}

// LaidOutContent describes raw content that has been placed within the
// encompassing structure and volume
//
// TODO: this can't have "$kernel:" refs at this point, fail in validate for
// bare structures with "$kernel:" refs
type LaidOutContent struct {
	*VolumeContent

	// StartOffset defines the start offset of this content image
	StartOffset quantity.Offset
	// Size is the maximum size occupied by this image
	Size quantity.Size
	// Index of the content in structure declaration inside gadget YAML
	Index int
}

func (p LaidOutContent) String() string {
	if p.Image != "" {
		return fmt.Sprintf("#%v (%q@%#x{%v})", p.Index, p.Image, p.StartOffset, p.Size)
	}
	return fmt.Sprintf("#%v (source:%q)", p.Index, p.UnresolvedSource)
}

type ResolvedContent struct {
	*VolumeContent

	// ResolvedSource is the absolute path of the Source after resolving
	// any references (e.g. to a "$kernel:" snap).
	ResolvedSource string

	// KernelUpdate is true if this content comes from the kernel
	// and has the "Update" property set
	KernelUpdate bool
}

func layoutVolumeStructures(volume *Volume) (structures []LaidOutStructure, err error) {
	structures = make([]LaidOutStructure, len(volume.Structure))

	// Even although we do not have the final offset as that depends on the
	// state of the installation disk and we do not know at this point, we
	// need some value for StartOffset so we can perform some validations.
	// We will overwrite the final offsets later.
	offset := quantity.Offset(0)
	for idx := range volume.Structure {
		ps := LaidOutStructure{
			VolumeStructure: &volume.Structure[idx],
		}

		if volume.Structure[idx].Offset != nil {
			offset = *volume.Structure[idx].Offset
		}
		// Fill the parts of OnDiskStructure that do not depend on the disk
		// or on whether we are encrypting or not.
		// TODO Eventually fill everything here by passing all needed info
		ps.OnDiskStructure = OnDiskStructure{
			Name:        ps.VolumeStructure.Name,
			Type:        ps.VolumeStructure.Type,
			StartOffset: offset,
			Size:        ps.VolumeStructure.Size,
		}

		offset += quantity.Offset(volume.Structure[idx].Size)
		// Note that structures are ordered by offset as volume.Structure
		// was ordered when reading the gadget information.
		structures[idx] = ps
	}

	// Check:
	// - No structure overlaps
	// - offset-write meets the restrictions defined in the gadget
	// TODO Reuse fully validateCrossVolumeStructure
	previousEnd := quantity.Offset(0)
	for idx, ps := range structures {
		if ps.StartOffset < previousEnd {
			return nil, fmt.Errorf("cannot lay out volume, structure %v overlaps with preceding structure %v", ps, structures[idx-1])
		}
		previousEnd = ps.StartOffset + quantity.Offset(ps.VolumeStructure.Size)

		if err := validateOffsetWrite(ps.VolumeStructure, structures[0].VolumeStructure, volume.MinSize()); err != nil {
			return nil, err
		}
	}

	return structures, nil
}

// LayoutVolumePartially attempts to lay out only the structures in the volume.
func LayoutVolumePartially(volume *Volume) (*PartiallyLaidOutVolume, error) {
	structures, err := layoutVolumeStructures(volume)
	if err != nil {
		return nil, err
	}

	vol := &PartiallyLaidOutVolume{
		Volume:           volume,
		LaidOutStructure: structures,
	}
	return vol, nil
}

func setOnDiskLabelAndTypeInLaidOuts(los []LaidOutStructure, encType secboot.EncryptionType) {
	for i := range los {
		los[i].PartitionFSLabel = los[i].Label()
		los[i].PartitionFSType = los[i].Filesystem()
		if encType != secboot.EncryptionTypeNone {
			switch los[i].Role() {
			case SystemData, SystemSave:
				los[i].PartitionFSLabel += "-enc"
				los[i].PartitionFSType = "crypto_LUKS"
			}
		}
	}
}

// LayoutVolume attempts to completely lay out the volume, that is the
// structures and their content, using provided options.
func LayoutVolume(volume *Volume, opts *LayoutOptions) (*LaidOutVolume, error) {
	var err error
	if opts == nil {
		opts = &LayoutOptions{}
	}
	doResolveContent := !(opts.IgnoreContent || opts.SkipResolveContent)

	var kernelInfo *kernel.Info
	if doResolveContent {
		// TODO:UC20: check and error if kernelRootDir == "" here
		// This needs the upper layer of gadget updates to be
		// updated to pass the kernel root first.
		//
		// Note that the kernelRootDir may reference the running
		// kernel if there is a gadget update or the new kernel if
		// there is a kernel update.
		kernelInfo, err = kernel.ReadInfo(opts.KernelRootDir)
		if err != nil {
			return nil, err
		}
	}

	structures, err := layoutVolumeStructures(volume)
	if err != nil {
		return nil, err
	}

	for idx := range structures {
		// Set appropriately label and type details
		// TODO: set this in layoutVolumeStructures in the future.
		setOnDiskLabelAndTypeInLaidOuts(structures, opts.EncType)

		// Lay out raw content. This can be skipped when only partition
		// creation is needed and is safe because each volume structure
		// has a size so even without the structure content the layout
		// can be calculated.
		if !opts.IgnoreContent {
			content, err := layOutStructureContent(opts.GadgetRootDir, &structures[idx])
			if err != nil {
				return nil, err
			}
			structures[idx].LaidOutContent = content
		}

		// resolve filesystem content
		if doResolveContent {
			resolvedContent, err := resolveVolumeContent(opts.GadgetRootDir, opts.KernelRootDir, kernelInfo, &structures[idx], nil)
			if err != nil {
				return nil, err
			}
			structures[idx].ResolvedContent = resolvedContent
		}
	}

	vol := &LaidOutVolume{
		Volume:           volume,
		LaidOutStructure: structures,
	}
	return vol, nil
}

func resolveVolumeContent(gadgetRootDir, kernelRootDir string, kernelInfo *kernel.Info, ps *LaidOutStructure, filter ResolvedContentFilterFunc) ([]ResolvedContent, error) {
	if !ps.HasFilesystem() {
		// structures without a file system are not resolved here
		return nil, nil
	}
	if len(ps.VolumeStructure.Content) == 0 {
		return nil, nil
	}

	content := make([]ResolvedContent, 0, len(ps.VolumeStructure.Content))
	for idx := range ps.VolumeStructure.Content {
		resolvedSource, kupdate, err := resolveContentPathOrRef(gadgetRootDir, kernelRootDir, kernelInfo, ps.VolumeStructure.Content[idx].UnresolvedSource)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve content for structure %v at index %v: %v", ps, idx, err)
		}
		rc := ResolvedContent{
			VolumeContent:  &ps.VolumeStructure.Content[idx],
			ResolvedSource: resolvedSource,
			KernelUpdate:   kupdate,
		}
		if filter != nil && !filter(&rc) {
			continue
		}
		content = append(content, rc)
	}

	return content, nil
}

// resolveContentPathOrRef resolves the relative path from gadget
// assets and any "$kernel:" references from "pathOrRef" using the
// provided gadget/kernel directories and the kernel info. It returns
// an absolute path, a flag indicating whether the content is part of
// a kernel update, or an error.
func resolveContentPathOrRef(gadgetRootDir, kernelRootDir string, kernelInfo *kernel.Info, pathOrRef string) (resolved string, kupdate bool, err error) {

	// TODO: add kernelRootDir == "" error too once all the higher
	//       layers in devicestate call gadget.Update() with a
	//       kernel dir set
	switch {
	case gadgetRootDir == "":
		return "", false, fmt.Errorf("internal error: gadget root dir cannot beempty")
	case pathOrRef == "":
		return "", false, fmt.Errorf("cannot use empty source")
	}

	// content may refer to "$kernel:<name>/<content>"
	var resolvedSource string
	if strings.HasPrefix(pathOrRef, "$kernel:") {
		wantedAsset, wantedContent, err := splitKernelRef(pathOrRef)
		if err != nil {
			return "", false, fmt.Errorf("cannot parse kernel ref: %v", err)
		}
		kernelAsset, ok := kernelInfo.Assets[wantedAsset]
		if !ok {
			return "", false, fmt.Errorf("cannot find %q in kernel info from %q", wantedAsset, kernelRootDir)
		}
		// look for exact content match or for a directory prefix match
		found := false
		for _, kcontent := range kernelAsset.Content {
			if wantedContent == kcontent {
				found = true
				break
			}
			// ensure we only check subdirs
			suffix := ""
			if !strings.HasSuffix(kcontent, "/") {
				suffix = "/"
			}
			if strings.HasPrefix(wantedContent, kcontent+suffix) {
				found = true
				break
			}
		}
		if !found {
			return "", false, fmt.Errorf("cannot find wanted kernel content %q in %q", wantedContent, kernelRootDir)
		}
		resolvedSource = filepath.Join(kernelRootDir, wantedContent)
		kupdate = kernelAsset.Update
	} else {
		resolvedSource = filepath.Join(gadgetRootDir, pathOrRef)
	}

	// restore trailing / if one was there
	if strings.HasSuffix(pathOrRef, "/") {
		resolvedSource += "/"
	}

	return resolvedSource, kupdate, nil
}

type byContentStartOffset []LaidOutContent

func (b byContentStartOffset) Len() int           { return len(b) }
func (b byContentStartOffset) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byContentStartOffset) Less(i, j int) bool { return b[i].StartOffset < b[j].StartOffset }

func getImageSize(path string) (quantity.Size, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return quantity.Size(stat.Size()), nil
}

func layOutStructureContent(gadgetRootDir string, ps *LaidOutStructure) ([]LaidOutContent, error) {
	if ps.HasFilesystem() {
		// structures with a filesystem do not need any extra layout
		return nil, nil
	}
	if len(ps.VolumeStructure.Content) == 0 {
		return nil, nil
	}

	content := make([]LaidOutContent, len(ps.VolumeStructure.Content))
	previousEnd := quantity.Offset(0)

	for idx, c := range ps.VolumeStructure.Content {
		imageSize, err := getImageSize(filepath.Join(gadgetRootDir, c.Image))
		if err != nil {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q: %v", ps, c.Image, err)
		}

		var start quantity.Offset
		if c.Offset != nil {
			start = *c.Offset
		} else {
			start = previousEnd
		}

		actualSize := imageSize

		if c.Size != 0 {
			if c.Size < imageSize {
				return nil, fmt.Errorf("cannot lay out structure %v: content %q size %v is larger than declared %v", ps, c.Image, actualSize, c.Size)
			}
			actualSize = c.Size
		}

		content[idx] = LaidOutContent{
			VolumeContent: &ps.VolumeStructure.Content[idx],
			Size:          actualSize,
			StartOffset:   ps.StartOffset + start,
			Index:         idx,
		}
		previousEnd = start + quantity.Offset(actualSize)
		if quantity.Size(previousEnd) > ps.VolumeStructure.Size {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q does not fit in the structure", ps, c.Image)
		}
	}

	sort.Sort(byContentStartOffset(content))

	previousEnd = ps.StartOffset
	for idx, pc := range content {
		if pc.StartOffset < previousEnd {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q overlaps with preceding image %q", ps, pc.Image, content[idx-1].Image)
		}
		previousEnd = pc.StartOffset + quantity.Offset(pc.Size)
	}

	return content, nil
}

// ShiftStructureTo translates the starting offset of a laid out structure and
// its content to the provided offset.
func ShiftStructureTo(ps LaidOutStructure, offset quantity.Offset) LaidOutStructure {
	change := int64(offset - ps.StartOffset)

	newPs := ps
	newPs.StartOffset = quantity.Offset(int64(ps.StartOffset) + change)

	newPs.LaidOutContent = make([]LaidOutContent, len(ps.LaidOutContent))
	for idx, pc := range ps.LaidOutContent {
		newPc := pc
		newPc.StartOffset = quantity.Offset(int64(pc.StartOffset) + change)
		newPs.LaidOutContent[idx] = newPc
	}
	return newPs
}

func isLayoutCompatible(current, new *Volume) error {
	if current.ID != new.ID {
		return fmt.Errorf("incompatible ID change from %v to %v", current.ID, new.ID)
	}
	if current.Schema != new.Schema {
		return fmt.Errorf("incompatible schema change from %v to %v",
			current.Schema, new.Schema)
	}
	if current.Bootloader != new.Bootloader {
		return fmt.Errorf("incompatible bootloader change from %v to %v",
			current.Bootloader, new.Bootloader)
	}

	// XXX: the code below asssumes both volumes have the same number of
	// structures, this limitation may be lifted later
	if len(current.Structure) != len(new.Structure) {
		return fmt.Errorf("incompatible change in the number of structures from %v to %v",
			len(current.Structure), len(new.Structure))
	}

	// at the structure level we expect the volume to be identical
	for i := range current.Structure {
		if err := canUpdateStructure(current.Structure, i, new.Structure, i, new.Schema); err != nil {
			return fmt.Errorf("incompatible structure #%d (%q) change: %v", new.Structure[i].YamlIndex, new.Structure[i].Name, err)
		}
	}
	return nil
}
