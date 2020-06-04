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
	"time"
)

type ValidationState = validationState

type LsblkFilesystemInfo = lsblkFilesystemInfo
type LsblkBlockDevice = lsblkBlockDevice
type SFDiskPartitionTable = sfdiskPartitionTable
type SFDiskPartition = sfdiskPartition

var (
	ValidateStructureType   = validateStructureType
	ValidateVolumeStructure = validateVolumeStructure
	ValidateRole            = validateRole
	ValidateVolume          = validateVolume

	ResolveVolume      = resolveVolume
	CanUpdateStructure = canUpdateStructure
	CanUpdateVolume    = canUpdateVolume

	EncodeLabel = encodeLabel

	WriteFile      = writeFileOrSymlink
	WriteDirectory = writeDirectory

	RawContentBackupPath = rawContentBackupPath

	UpdaterForStructure = updaterForStructure

	EnsureVolumeConsistency = ensureVolumeConsistency

	Flatten = flatten

	FilesystemInfo                 = filesystemInfo
	BuildPartitionList             = buildPartitionList
	Mkfs                           = mkfs
	EnsureNodesExist               = ensureNodesExist
	DeviceLayoutFromPartitionTable = deviceLayoutFromPartitionTable
	ListCreatedPartitions          = listCreatedPartitions
)

func MockEvalSymlinks(mock func(path string) (string, error)) (restore func()) {
	oldEvalSymlinks := evalSymlinks
	evalSymlinks = mock
	return func() {
		evalSymlinks = oldEvalSymlinks
	}
}

func MockMkfsHandlers(mock map[string]MkfsFunc) (restore func()) {
	old := mkfsHandlers
	mkfsHandlers = mock
	return func() {
		mkfsHandlers = old
	}
}

func MockEnsureNodesExist(f func(dss []OnDiskStructure, timeout time.Duration) error) (restore func()) {
	old := ensureNodesExist
	ensureNodesExist = f
	return func() {
		ensureNodesExist = old
	}
}

func MockDeployMountpoint(new string) (restore func()) {
	old := deployMountpoint
	deployMountpoint = new
	return func() {
		deployMountpoint = old
	}
}

func MockSysMount(f func(source, target, fstype string, flags uintptr, data string) error) (restore func()) {
	old := sysMount
	sysMount = f
	return func() {
		sysMount = old
	}
}

func MockSysUnmount(f func(target string, flags int) error) (restore func()) {
	old := sysUnmount
	sysUnmount = f
	return func() {
		sysUnmount = old
	}
}
