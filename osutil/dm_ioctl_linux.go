// -*- Mode: Go; indent-tabs-mode: t -*-

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

package osutil

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

var (
	osOpenFile = os.OpenFile
)

const (
	DM_VERSION_MAJOR      = 4
	DM_VERSION_MINOR      = 0
	DM_VERSION_PATCHLEVEL = 0
)

func dmIoctlImpl(fd uintptr, command int, data unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(command), uintptr(data))
	if errno != 0 {
		return os.NewSyscallError("ioctl", errno)
	}
	return nil
}

var dmIoctl = dmIoctlImpl

type TargetInfo = struct {
	SectorStart uint64
	Length      uint64
	Status      int32
	TargetType  string
	Params      string
}

func DmIoctlTableStatus(major uint32, minor uint32) ([]TargetInfo, error) {
	dmControl, err := osOpenFile("/dev/mapper/control", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open /dev/mapper/control: %w", err)
	}
	defer dmControl.Close()

	var data_len uint32
	data_len = 4096
	buf := bytes.NewBuffer([]byte{})
	ioctlReq := unix.DmIoctl{
		Version:      [3]uint32{DM_VERSION_MAJOR, DM_VERSION_MINOR, DM_VERSION_PATCHLEVEL},
		Data_size:    data_len,
		Data_start:   unix.SizeofDmIoctl,
		Target_count: 1,
		Flags:        unix.DM_STATUS_TABLE_FLAG,
		Event_nr:     0,
		Dev:          unix.Mkdev(major, minor),
	}

	binary.Write(buf, Endian(), ioctlReq)
	data := make([]byte, data_len)
	copy(data, buf.Bytes())

	if err := dmIoctl(dmControl.Fd(), unix.DM_TABLE_STATUS, unsafe.Pointer(&data[0])); err != nil {
		return nil, err
	}

	ioctlResp := unix.DmIoctl{}
	binary.Read(bytes.NewReader(data), Endian(), &ioctlResp)

	if (ioctlResp.Flags & unix.DM_BUFFER_FULL_FLAG) != 0 {
		return nil, fmt.Errorf("table was too big for buffer")
	}

	realDataBuf := data[ioctlResp.Data_start:ioctlResp.Data_size]
	realData := bytes.NewReader(realDataBuf)
	var targets []TargetInfo

	for i := uint32(0); i < ioctlResp.Target_count; i++ {
		targetSpec := unix.DmTargetSpec{}
		binary.Read(realData, Endian(), &targetSpec)
		current, err := realData.Seek(0, 1)
		if err != nil {
			return nil, err
		}
		extra := make([]byte, targetSpec.Next-uint32(current))
		if _, err := realData.Read(extra); err != nil {
			return nil, err
		}
		targets = append(targets, TargetInfo{
			SectorStart: targetSpec.Sector_start,
			Length:      targetSpec.Length,
			Status:      targetSpec.Status,
			TargetType:  string(bytes.TrimRight(targetSpec.Target_type[:], "\x00")),
			Params:      string(bytes.TrimRight(extra, "\x00")),
		})
	}

	return targets, nil
}
