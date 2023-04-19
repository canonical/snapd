// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package udev

//#cgo CFLAGS: -D_FILE_OFFSET_BITS=64
//#cgo pkg-config: libudev
//#cgo LDFLAGS:
//
//#include <stdlib.h>
//#include <libudev.h>
import "C"

import (
	"unsafe"
)

type AbstractUdevDevice interface {
	Close()
	GetParentWithSubsystemDevtype(subsystem string, devtype *string) AbstractUdevDevice
}

type AbstractUdev interface {
	Close()
	DeviceNewFromEnvironment() (AbstractUdevDevice, error)
}

type UdevImpl struct {
	ptr *C.struct_udev
}

type UdevDeviceImpl struct {
	ptr *C.struct_udev_device
}

func (u *UdevImpl) DeviceNewFromEnvironment() (AbstractUdevDevice, error) {
	ptr, err := C.udev_device_new_from_environment(u.ptr)
	if ptr == nil {
		return nil, err
	}
	return &UdevDeviceImpl{ptr}, nil
}

func (u *UdevImpl) Close() {
	C.udev_unref(u.ptr)
}

func (d *UdevDeviceImpl) Close() {
	C.udev_device_unref(d.ptr)
}

func (d *UdevDeviceImpl) GetParentWithSubsystemDevtype(subsystem string, devtype *string) AbstractUdevDevice {
	csubsystem := C.CString(subsystem)
	defer C.free(unsafe.Pointer(csubsystem))
	var cdevtype *C.char
	if devtype != nil {
		cdevtype = C.CString(subsystem)
		defer C.free(unsafe.Pointer(cdevtype))
	}
	parentDevice := C.udev_device_get_parent_with_subsystem_devtype(d.ptr, csubsystem, cdevtype)
	if parentDevice == nil {
		return nil
	}
	return &UdevDeviceImpl{parentDevice}
}

func newUdevImpl() (AbstractUdev, error) {
	ptr, err := C.udev_new()
	if ptr == nil {
		return nil, err
	}
	return &UdevImpl{ptr}, nil
}

var NewUdev = newUdevImpl
