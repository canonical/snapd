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

	"github.com/snapcore/snapd/gadget/quantity"
)

// LayoutConstraints defines the constraints for arranging structures within a
// volume
type LayoutConstraints struct {
	// NonMBRStartOffset is the default start offset of non-MBR structure in
	// the volume.
	NonMBRStartOffset quantity.Size
	// SectorSize is the size of the sector to be used for calculations
	SectorSize quantity.Size
}

// LaidOutVolume defines the size of a volume and arrangement of all the
// structures within it
type LaidOutVolume struct {
	*Volume
	// Size is the total size of the volume
	Size quantity.Size
	// SectorSize sector size of the volume
	SectorSize quantity.Size
	// LaidOutStructure is a list of structures within the volume, sorted
	// by their start offsets
	LaidOutStructure []LaidOutStructure
	// RootDir is the root directory for volume data
	RootDir string
}

// PartiallyLaidOutVolume defines the layout of volume structures, but lacks the
// details about the layout of raw image content within the bare structures.
type PartiallyLaidOutVolume struct {
	*Volume
	// SectorSize sector size of the volume
	SectorSize quantity.Size
	// LaidOutStructure is a list of structures within the volume, sorted
	// by their start offsets
	LaidOutStructure []LaidOutStructure
}

// LaidOutStructure describes a VolumeStructure that has been placed within the
// volume
type LaidOutStructure struct {
	*VolumeStructure
	// StartOffset defines the start offset of the structure within the
	// enclosing volume
	StartOffset quantity.Size
	// AbsoluteOffsetWrite is the resolved absolute position of offset-write
	// for this structure element within the enclosing volume
	AbsoluteOffsetWrite *quantity.Size
	// Index of the structure definition in gadget YAML
	Index int
	// LaidOutContent is a list of raw content inside the structure
	LaidOutContent []LaidOutContent
}

func (p LaidOutStructure) String() string {
	return fmtIndexAndName(p.Index, p.Name)
}

type byStartOffset []LaidOutStructure

func (b byStartOffset) Len() int           { return len(b) }
func (b byStartOffset) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byStartOffset) Less(i, j int) bool { return b[i].StartOffset < b[j].StartOffset }

// LaidOutContent describes raw content that has been placed within the
// encompassing structure and volume
type LaidOutContent struct {
	*VolumeContent

	// StartOffset defines the start offset of this content image
	StartOffset quantity.Size
	// AbsoluteOffsetWrite is the resolved absolute position of offset-write
	// for this content element within the enclosing volume
	AbsoluteOffsetWrite *quantity.Size
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

func layoutVolumeStructures(volume *Volume, constraints LayoutConstraints) (structures []LaidOutStructure, byName map[string]*LaidOutStructure, err error) {
	previousEnd := quantity.Size(0)
	structures = make([]LaidOutStructure, len(volume.Structure))
	byName = make(map[string]*LaidOutStructure, len(volume.Structure))

	if constraints.SectorSize == 0 {
		return nil, nil, fmt.Errorf("cannot lay out volume, invalid constraints: sector size cannot be 0")
	}

	for idx, s := range volume.Structure {
		var start quantity.Size
		if s.Offset == nil {
			if s.Role != schemaMBR && previousEnd < constraints.NonMBRStartOffset {
				start = constraints.NonMBRStartOffset
			} else {
				start = previousEnd
			}
		} else {
			start = *s.Offset
		}

		end := start + s.Size
		ps := LaidOutStructure{
			VolumeStructure: &volume.Structure[idx],
			StartOffset:     start,
			Index:           idx,
		}

		if ps.Role != schemaMBR {
			if s.Size%constraints.SectorSize != 0 {
				return nil, nil, fmt.Errorf("cannot lay out volume, structure %v size is not a multiple of sector size %v",
					ps, constraints.SectorSize)
			}
		}

		if ps.Name != "" {
			byName[ps.Name] = &ps
		}

		structures[idx] = ps

		previousEnd = end
	}

	// sort by starting offset
	sort.Sort(byStartOffset(structures))

	previousEnd = quantity.Size(0)
	for idx, ps := range structures {
		if ps.StartOffset < previousEnd {
			return nil, nil, fmt.Errorf("cannot lay out volume, structure %v overlaps with preceding structure %v", ps, structures[idx-1])
		}
		previousEnd = ps.StartOffset + ps.Size

		offsetWrite, err := resolveOffsetWrite(ps.OffsetWrite, byName)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot resolve offset-write of structure %v: %v", ps, err)
		}
		structures[idx].AbsoluteOffsetWrite = offsetWrite
	}

	return structures, byName, nil
}

// LayoutVolumePartially attempts to lay out only the structures in the volume using provided constraints
func LayoutVolumePartially(volume *Volume, constraints LayoutConstraints) (*PartiallyLaidOutVolume, error) {
	structures, _, err := layoutVolumeStructures(volume, constraints)
	if err != nil {
		return nil, err
	}
	vol := &PartiallyLaidOutVolume{
		Volume:           volume,
		SectorSize:       constraints.SectorSize,
		LaidOutStructure: structures,
	}
	return vol, nil
}

// LayoutVolume attempts to completely lay out the volume, that is the
// structures and their content, using provided constraints
func LayoutVolume(gadgetRootDir string, volume *Volume, constraints LayoutConstraints) (*LaidOutVolume, error) {

	structures, byName, err := layoutVolumeStructures(volume, constraints)
	if err != nil {
		return nil, err
	}

	farthestEnd := quantity.Size(0)
	fartherstOffsetWrite := quantity.Size(0)

	for idx, ps := range structures {
		if ps.AbsoluteOffsetWrite != nil && *ps.AbsoluteOffsetWrite > fartherstOffsetWrite {
			fartherstOffsetWrite = *ps.AbsoluteOffsetWrite
		}
		if end := ps.StartOffset + ps.Size; end > farthestEnd {
			farthestEnd = end
		}

		content, err := layOutStructureContent(gadgetRootDir, &structures[idx], byName)
		if err != nil {
			return nil, err
		}

		for _, c := range content {
			if c.AbsoluteOffsetWrite != nil && *c.AbsoluteOffsetWrite > fartherstOffsetWrite {
				fartherstOffsetWrite = *c.AbsoluteOffsetWrite
			}
		}

		structures[idx].LaidOutContent = content
	}

	volumeSize := farthestEnd
	if fartherstOffsetWrite+SizeLBA48Pointer > farthestEnd {
		volumeSize = fartherstOffsetWrite + SizeLBA48Pointer
	}

	vol := &LaidOutVolume{
		Volume:           volume,
		Size:             volumeSize,
		SectorSize:       constraints.SectorSize,
		LaidOutStructure: structures,
		RootDir:          gadgetRootDir,
	}
	return vol, nil
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

func layOutStructureContent(gadgetRootDir string, ps *LaidOutStructure, known map[string]*LaidOutStructure) ([]LaidOutContent, error) {
	if ps.HasFilesystem() {
		// structures with a filesystem do not need any extra layout
		return nil, nil
	}
	if len(ps.Content) == 0 {
		return nil, nil
	}

	content := make([]LaidOutContent, len(ps.Content))
	previousEnd := quantity.Size(0)

	for idx, c := range ps.Content {
		imageSize, err := getImageSize(filepath.Join(gadgetRootDir, c.Image))
		if err != nil {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q: %v", ps, c.Image, err)
		}

		var start quantity.Size
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

		offsetWrite, err := resolveOffsetWrite(c.OffsetWrite, known)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve offset-write of structure %v content %q: %v", ps, c.Image, err)
		}

		content[idx] = LaidOutContent{
			VolumeContent: &ps.Content[idx],
			Size:          actualSize,
			StartOffset:   ps.StartOffset + start,
			Index:         idx,
			// break for gofmt < 1.11
			AbsoluteOffsetWrite: offsetWrite,
		}
		previousEnd = start + actualSize
		if previousEnd > ps.Size {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q does not fit in the structure", ps, c.Image)
		}
	}

	sort.Sort(byContentStartOffset(content))

	previousEnd = ps.StartOffset
	for idx, pc := range content {
		if pc.StartOffset < previousEnd {
			return nil, fmt.Errorf("cannot lay out structure %v: content %q overlaps with preceding image %q", ps, pc.Image, content[idx-1].Image)
		}
		previousEnd = pc.StartOffset + pc.Size
	}

	return content, nil
}

func resolveOffsetWrite(offsetWrite *RelativeOffset, knownStructs map[string]*LaidOutStructure) (*quantity.Size, error) {
	if offsetWrite == nil {
		return nil, nil
	}

	var relativeToOffset quantity.Size
	if offsetWrite.RelativeTo != "" {
		otherStruct, ok := knownStructs[offsetWrite.RelativeTo]
		if !ok {
			return nil, fmt.Errorf("refers to an unknown structure %q", offsetWrite.RelativeTo)
		}
		relativeToOffset = otherStruct.StartOffset
	}

	resolvedOffsetWrite := relativeToOffset + offsetWrite.Offset
	return &resolvedOffsetWrite, nil
}

// ShiftStructureTo translates the starting offset of a laid out structure and
// its content to the provided offset.
func ShiftStructureTo(ps LaidOutStructure, offset quantity.Size) LaidOutStructure {
	change := int64(offset - ps.StartOffset)

	newPs := ps
	newPs.StartOffset = quantity.Size(int64(ps.StartOffset) + change)

	newPs.LaidOutContent = make([]LaidOutContent, len(ps.LaidOutContent))
	for idx, pc := range ps.LaidOutContent {
		newPc := pc
		newPc.StartOffset = quantity.Size(int64(pc.StartOffset) + change)
		newPs.LaidOutContent[idx] = newPc
	}
	return newPs
}

func isLayoutCompatible(current, new *PartiallyLaidOutVolume) error {
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
	// structures, this limitation may be lifter later
	if len(current.LaidOutStructure) != len(new.LaidOutStructure) {
		return fmt.Errorf("incompatible change in the number of structures from %v to %v",
			len(current.LaidOutStructure), len(new.LaidOutStructure))
	}

	// at the structure level we expect the volume to be identical
	for i := range current.LaidOutStructure {
		from := &current.LaidOutStructure[i]
		to := &new.LaidOutStructure[i]
		if err := canUpdateStructure(from, to, new.Schema); err != nil {
			return fmt.Errorf("incompatible structure %v change: %v", to, err)
		}
	}
	return nil
}
