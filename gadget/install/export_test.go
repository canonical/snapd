// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package install

import (
	"fmt"
	"time"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
)

type MkfsParams = mkfsParams

var (
	MakeFilesystem         = makeFilesystem
	WriteFilesystemContent = writeFilesystemContent
	MountFilesystem        = mountFilesystem

	BuildPartitionList      = buildPartitionList
	RemoveCreatedPartitions = removeCreatedPartitions
	EnsureNodesExist        = ensureNodesExist

	CreatedDuringInstall        = createdDuringInstall
	TestCreateMissingPartitions = createMissingPartitions
)

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

func MockEnsureNodesExist(f func(nodes []string, timeout time.Duration) error) (restore func()) {
	old := ensureNodesExist
	ensureNodesExist = f
	return func() {
		ensureNodesExist = old
	}
}

func MockMkfsMake(f func(typ, img, label string, devSize, sectorSize quantity.Size) error) (restore func()) {
	old := mkfsImpl
	mkfsImpl = f
	return func() {
		mkfsImpl = old
	}
}

func MockKernelEnsureKernelDriversTree(f func(kMntPts kernel.MountPoints, compsMntPts []kernel.ModulesCompMountPoints, destDir string, opts *kernel.KernelDriversTreeOptions) (err error)) (restore func()) {
	old := kernelEnsureKernelDriversTree
	kernelEnsureKernelDriversTree = f
	return func() {
		kernelEnsureKernelDriversTree = old
	}
}

func CheckEncryptionSetupData(encryptSetup *EncryptionSetupData, labelToEncDevice map[string]string) error {
	for label, part := range encryptSetup.parts {
		switch part.role {
		case gadget.SystemData, gadget.SystemSave:
			// ok
		default:
			return fmt.Errorf("unexpected role in %q: %q", label, part.role)
		}
		if part.encryptedDevice != labelToEncDevice[label] {
			return fmt.Errorf("encrypted device in EncryptionSetupData (%q) different to expected (%q)",
				encryptSetup.parts[label].encryptedDevice, labelToEncDevice[label])
		}
		if len(part.encryptionKey) == 0 {
			return fmt.Errorf("encryption key for %q is empty", label)
		}
	}

	return nil
}
