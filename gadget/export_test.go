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

import "github.com/snapcore/snapd/gadget/quantity"

type (
	MountedFilesystemUpdater = mountedFilesystemUpdater
	RawStructureUpdater      = rawStructureUpdater
	InvalidOffsetError       = invalidOffsetError
)

var (
	ValidateStructureType   = validateStructureType
	ValidateVolumeStructure = validateVolumeStructure
	ValidateRole            = validateRole
	ValidateVolume          = validateVolume
	ValidateOffsetWrite     = validateOffsetWrite

	SetImplicitForVolumeStructure = setImplicitForVolumeStructure

	ResolveVolume      = resolveVolume
	CanUpdateStructure = canUpdateStructure
	CanUpdateVolume    = canUpdateVolume

	WriteFile = writeFileOrSymlink

	RawContentBackupPath = rawContentBackupPath

	UpdaterForStructure = updaterForStructure

	Flatten = flatten

	NewRawStructureUpdater      = newRawStructureUpdater
	NewMountedFilesystemUpdater = newMountedFilesystemUpdater

	ParseRelativeOffset = parseRelativeOffset

	SplitKernelRef = splitKernelRef

	ResolveVolumeContent = resolveVolumeContent

	GadgetVolumeConsumesOneKernelUpdateAsset = gadgetVolumeConsumesOneKernelUpdateAsset
	GadgetVolumeKernelUpdateAssetsConsumed   = gadgetVolumeKernelUpdateAssetsConsumed

	BuildNewVolumeToDeviceMapping = buildNewVolumeToDeviceMapping
	ErrSkipUpdateProceedRefresh   = errSkipUpdateProceedRefresh

	BuildVolumeStructureToLocation = buildVolumeStructureToLocation
	VolumeStructureToLocationMap   = volumeStructureToLocationMap

	OnDiskStructureIsLikelyImplicitSystemDataRole = onDiskStructureIsLikelyImplicitSystemDataRole

	SearchVolumeWithTraitsAndMatchParts = searchVolumeWithTraitsAndMatchParts
	OrderStructuresByOffset             = orderStructuresByOffset
	LayoutVolumePartially               = layoutVolumePartially
)

func MockEvalSymlinks(mock func(path string) (string, error)) (restore func()) {
	oldEvalSymlinks := evalSymlinks
	evalSymlinks = mock
	return func() {
		evalSymlinks = oldEvalSymlinks
	}
}

func (m *MountedFilesystemWriter) WriteDirectory(volumeRoot, src, dst string, preserveInDst []string) error {
	return m.writeDirectory(volumeRoot, src, dst, preserveInDst)
}

// to test handling of unknown keys when we un-marshal
func (s *StructureEncryptionParameters) SetUnknownKeys(m map[string]string) {
	s.unknownKeys = m
}

func NewInvalidOffsetError(offset, lowerBound, upperBound quantity.Offset) *InvalidOffsetError {
	return &invalidOffsetError{offset: offset, lowerBound: lowerBound, upperBound: upperBound}
}

func (v *Volume) YamlIdxToStructureIdx(yamlIdx int) (int, error) {
	return v.yamlIdxToStructureIdx(yamlIdx)
}
