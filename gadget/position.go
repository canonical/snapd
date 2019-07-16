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
)

// PositioningConstraints defines the constraints for positioning structures
// within a volume
type PositioningConstraints struct {
	// NonMBRStartOffset is the default start offset of non-MBR structure in
	// the volume.
	NonMBRStartOffset Size
	// SectorSize is the size of the sector to be used for calculations
	SectorSize Size
}

// PositionedVolume defines the size of a volume and positions of all the
// structures within it
type PositionedVolume struct {
	*Volume
	// Size is the total size of the volume
	Size Size
	// SectorSize sector size of the volume
	SectorSize Size
	// PositionedStructure are sorted in order of 'appearance' in the volume
	PositionedStructure []PositionedStructure
	// RootDir is the root directory for volume data
	RootDir string
}

// PositionedStructure describes a VolumeStructure that has been positioned
// within the volume
type PositionedStructure struct {
	*VolumeStructure
	// StartOffset defines the start offset of the structure within the
	// enclosing volume
	StartOffset Size
	// PositionedOffsetWrite is the resolved position of offset-write for
	// this structure element within the enclosing volume
	PositionedOffsetWrite *Size
	// Index of the structure definition in gadget YAML
	Index int

	// PositionedContent is a list of raw content included in this structure
	PositionedContent []PositionedContent
}

func (p PositionedStructure) String() string {
	return fmtIndexAndName(p.Index, p.Name)
}

type byStartOffset []PositionedStructure

func (b byStartOffset) Len() int           { return len(b) }
func (b byStartOffset) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byStartOffset) Less(i, j int) bool { return b[i].StartOffset < b[j].StartOffset }

// PositionedContent describes raw content that has been positioned within the
// encompassing structure
type PositionedContent struct {
	*VolumeContent

	// StartOffset defines the start offset of this content image
	StartOffset Size
	// PositionedOffsetWrite is the resolved position of offset-write for
	// this content element within the enclosing volume
	PositionedOffsetWrite *Size
	// Size is the maximum size occupied by this image
	Size Size
	// Index of the content in structure declaration inside gadget YAML
	Index int
}

func (p PositionedContent) String() string {
	if p.Image != "" {
		return fmt.Sprintf("#%v (%q@%#x{%v})", p.Index, p.Image, p.StartOffset, p.Size)
	}
	return fmt.Sprintf("#%v (source:%q)", p.Index, p.Source)
}

// PositionVolume attempts to lay out the volume using constraints and returns a
// fully positioned description of the resulting volume
func PositionVolume(gadgetRootDir string, volume *Volume, constraints PositioningConstraints) (*PositionedVolume, error) {
	previousEnd := Size(0)
	farthestEnd := Size(0)
	fartherstOffsetWrite := Size(0)
	structures := make([]PositionedStructure, len(volume.Structure))
	structuresByName := make(map[string]*PositionedStructure, len(volume.Structure))

	if constraints.SectorSize == 0 {
		return nil, fmt.Errorf("cannot position volume, invalid constraints: sector size cannot be 0")
	}

	for idx, s := range volume.Structure {
		var start Size
		if s.Offset == nil {
			if s.EffectiveRole() != MBR && previousEnd < constraints.NonMBRStartOffset {
				start = constraints.NonMBRStartOffset
			} else {
				start = previousEnd
			}
		} else {
			start = *s.Offset
		}

		end := start + s.Size
		ps := PositionedStructure{
			VolumeStructure: &volume.Structure[idx],
			StartOffset:     start,
			Index:           idx,
		}

		if ps.EffectiveRole() != MBR {
			if s.Size%constraints.SectorSize != 0 {
				return nil, fmt.Errorf("cannot position volume, structure %v size is not a multiple of sector size %v",
					ps, constraints.SectorSize)
			}
		}

		if ps.Name != "" {
			structuresByName[ps.Name] = &ps
		}

		structures[idx] = ps

		if end > farthestEnd {
			farthestEnd = end
		}
		previousEnd = end
	}

	// sort by starting offset
	sort.Sort(byStartOffset(structures))

	previousEnd = Size(0)
	for idx, ps := range structures {
		if ps.StartOffset < previousEnd {
			return nil, fmt.Errorf("cannot position volume, structure %v overlaps with preceding structure %v", ps, structures[idx-1])
		}
		previousEnd = ps.StartOffset + ps.Size

		offsetWrite, err := resolveOffsetWrite(ps.OffsetWrite, structuresByName)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve offset-write of structure %v: %v", ps, err)
		}
		structures[idx].PositionedOffsetWrite = offsetWrite

		if offsetWrite != nil && *offsetWrite > fartherstOffsetWrite {
			fartherstOffsetWrite = *offsetWrite
		}

		content, err := positionStructureContent(gadgetRootDir, &structures[idx], structuresByName)
		if err != nil {
			return nil, err
		}

		for _, c := range content {
			if c.PositionedOffsetWrite != nil && *c.PositionedOffsetWrite > fartherstOffsetWrite {
				fartherstOffsetWrite = *c.PositionedOffsetWrite
			}
		}

		structures[idx].PositionedContent = content
	}

	volumeSize := farthestEnd
	if fartherstOffsetWrite+SizeLBA48Pointer > farthestEnd {
		volumeSize = fartherstOffsetWrite + SizeLBA48Pointer
	}

	vol := &PositionedVolume{
		Volume:              volume,
		Size:                volumeSize,
		SectorSize:          constraints.SectorSize,
		PositionedStructure: structures,
		RootDir:             gadgetRootDir,
	}
	return vol, nil
}

type byContentStartOffset []PositionedContent

func (b byContentStartOffset) Len() int           { return len(b) }
func (b byContentStartOffset) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byContentStartOffset) Less(i, j int) bool { return b[i].StartOffset < b[j].StartOffset }

func getImageSize(path string) (Size, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return Size(stat.Size()), nil
}

func positionStructureContent(gadgetRootDir string, ps *PositionedStructure, known map[string]*PositionedStructure) ([]PositionedContent, error) {
	if !ps.IsBare() {
		// structures with a filesystem do not need any extra
		// positioning
		return nil, nil
	}
	if len(ps.Content) == 0 {
		return nil, nil
	}

	content := make([]PositionedContent, len(ps.Content))
	previousEnd := Size(0)

	for idx, c := range ps.Content {
		imageSize, err := getImageSize(filepath.Join(gadgetRootDir, c.Image))
		if err != nil {
			return nil, fmt.Errorf("cannot position structure %v: content %q: %v", ps, c.Image, err)
		}

		var start Size
		if c.Offset != nil {
			start = *c.Offset
		} else {
			start = previousEnd
		}

		actualSize := imageSize

		if c.Size != 0 {
			if c.Size < imageSize {
				return nil, fmt.Errorf("cannot position structure %v: content %q size %v is larger than declared %v", ps, c.Image, actualSize, c.Size)
			}
			actualSize = c.Size
		}

		offsetWrite, err := resolveOffsetWrite(c.OffsetWrite, known)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve offset-write of structure %v content %q: %v", ps, c.Image, err)
		}

		content[idx] = PositionedContent{
			VolumeContent: &ps.Content[idx],
			Size:          actualSize,
			StartOffset:   ps.StartOffset + start,
			Index:         idx,
			// break for gofmt < 1.11
			PositionedOffsetWrite: offsetWrite,
		}
		previousEnd = start + actualSize
		if previousEnd > ps.Size {
			return nil, fmt.Errorf("cannot position structure %v: content %q does not fit in the structure", ps, c.Image)
		}
	}

	sort.Sort(byContentStartOffset(content))

	previousEnd = ps.StartOffset
	for idx, pc := range content {
		if pc.StartOffset < previousEnd {
			return nil, fmt.Errorf("cannot position structure %v: content %q overlaps with preceding image %q", ps, pc.Image, content[idx-1].Image)
		}
		previousEnd = pc.StartOffset + pc.Size
	}

	return content, nil
}

func resolveOffsetWrite(offsetWrite *RelativeOffset, knownStructs map[string]*PositionedStructure) (*Size, error) {
	if offsetWrite == nil {
		return nil, nil
	}

	var relativeToOffset Size
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

// ShiftStructureTo creates a new positioned structure, shifted to start at a
// given offset. The start offsets of positioned content within the structure is
// updated.
func ShiftStructureTo(ps PositionedStructure, offset Size) PositionedStructure {
	change := int64(offset - ps.StartOffset)

	newPs := ps
	newPs.StartOffset = Size(int64(ps.StartOffset) + change)

	newPs.PositionedContent = make([]PositionedContent, len(ps.PositionedContent))
	for idx, pc := range ps.PositionedContent {
		newPc := pc
		newPc.StartOffset = Size(int64(pc.StartOffset) + change)
		newPs.PositionedContent[idx] = newPc
	}
	return newPs
}
