// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2024 Canonical Ltd
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

package boot

import (
	"context"

	"github.com/canonical/go-efilib"
	"github.com/canonical/go-efilib/linux"

	"github.com/snapcore/snapd/testutil"
)

var (
	ConstructLoadOption      = constructLoadOption
	SetEfiBootOptionVariable = setEfiBootOptionVariable
	SetEfiBootOrderVariable  = setEfiBootOrderVariable
)

func MockEfiListVariables(f func(ctx context.Context) ([]efi.VariableDescriptor, error)) (restore func()) {
	restore = testutil.Backup(&efiListVariables)
	efiListVariables = f
	return restore
}

func MockEfiReadVariable(f func(ctx context.Context, name string, guid efi.GUID) ([]byte, efi.VariableAttributes, error)) (restore func()) {
	restore = testutil.Backup(&efiReadVariable)
	efiReadVariable = f
	return restore
}

func MockEfiWriteVariable(f func(ctx context.Context, name string, guid efi.GUID, attrs efi.VariableAttributes, data []byte) error) (restore func()) {
	restore = testutil.Backup(&efiWriteVariable)
	efiWriteVariable = f
	return restore
}

func MockLinuxFilePathToDevicePath(f func(path string, mode linux.FilePathToDevicePathMode) (out efi.DevicePath, err error)) (restore func()) {
	restore = testutil.Backup(&linuxFilePathToDevicePath)
	linuxFilePathToDevicePath = f
	return restore
}
