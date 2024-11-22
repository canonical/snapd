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

package osutil_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	. "gopkg.in/check.v1"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/osutil"
)

type dmIoctlSuite struct{}

var _ = Suite(&dmIoctlSuite{})

func (s *dmIoctlSuite) SetUpTest(c *C) {
}

func (s *dmIoctlSuite) TestDmIoctlHappy(c *C) {
	tmp := c.MkDir()
	fakeControl := filepath.Join(tmp, "control")
	c.Assert(os.WriteFile(fakeControl, []byte{}, 0644), IsNil)

	var controlFile *os.File

	restore := osutil.MockOsOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		c.Check(name, Equals, "/dev/mapper/control")
		c.Assert(controlFile, IsNil)
		f, err := os.OpenFile(fakeControl, flag, perm)
		c.Assert(err, IsNil)
		controlFile = f
		return controlFile, nil
	})
	defer restore()

	restore = osutil.MockDmIoctl(func(fd uintptr, command int, data unsafe.Pointer) error {
		c.Check(fd, Equals, controlFile.Fd())
		c.Check(command, Equals, unix.DM_TABLE_STATUS)
		buf := unsafe.Slice((*byte)(data), unix.SizeofDmIoctl)
		ioctl := unix.DmIoctl{}
		binary.Read(bytes.NewReader(buf), osutil.Endian(), &ioctl)

		c.Check(ioctl.Dev, Equals, unix.Mkdev(1, 2))

		extraData := unsafe.Slice((*byte)(unsafe.Add(data, ioctl.Data_start)), ioctl.Data_size-ioctl.Data_start)

		ioctl.Target_count = 1

		params := []byte("a b c d\x00")

		var targetType [16]byte
		copy(targetType[:], []byte("thetype\x00"))
		targetSpec := unix.DmTargetSpec{
			Target_type: targetType,
		}
		targetSpec.Next = uint32(unix.SizeofDmTargetSpec + len(params))

		outbuf := bytes.NewBuffer([]byte{})
		binary.Write(outbuf, osutil.Endian(), ioctl)
		copy(buf, outbuf.Bytes())
		outdata := bytes.NewBuffer([]byte{})
		binary.Write(outdata, osutil.Endian(), targetSpec)
		outdata.Write(params)
		c.Assert(outdata.Len() < len(extraData), Equals, true)
		copy(extraData, outdata.Bytes())

		return nil
	})
	defer restore()

	targetInfos, err := osutil.DmIoctlTableStatus(1, 2)
	c.Assert(err, IsNil)
	c.Assert(targetInfos, HasLen, 1)

	c.Check(targetInfos[0].TargetType, Equals, "thetype")
	c.Check(targetInfos[0].Params, Equals, "a b c d")
}

func (s *dmIoctlSuite) TestDmIoctlFullBuffer(c *C) {
	tmp := c.MkDir()
	fakeControl := filepath.Join(tmp, "control")
	c.Assert(os.WriteFile(fakeControl, []byte{}, 0644), IsNil)

	var controlFile *os.File

	restore := osutil.MockOsOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		c.Check(name, Equals, "/dev/mapper/control")
		c.Assert(controlFile, IsNil)
		f, err := os.OpenFile(fakeControl, flag, perm)
		c.Assert(err, IsNil)
		controlFile = f
		return controlFile, nil
	})
	defer restore()

	restore = osutil.MockDmIoctl(func(fd uintptr, command int, data unsafe.Pointer) error {
		buf := unsafe.Slice((*byte)(data), unix.SizeofDmIoctl)
		ioctl := unix.DmIoctl{}
		binary.Read(bytes.NewReader(buf), osutil.Endian(), &ioctl)

		ioctl.Flags = unix.DM_BUFFER_FULL_FLAG

		outbuf := bytes.NewBuffer([]byte{})
		binary.Write(outbuf, osutil.Endian(), ioctl)
		copy(buf, outbuf.Bytes())

		return nil
	})
	defer restore()

	_, err := osutil.DmIoctlTableStatus(1, 2)
	c.Assert(err, ErrorMatches, `table was too big for buffer`)
}

func (s *dmIoctlSuite) TestDmIoctlErrno(c *C) {
	tmp := c.MkDir()
	fakeControl := filepath.Join(tmp, "control")
	c.Assert(os.WriteFile(fakeControl, []byte{}, 0644), IsNil)

	restore := osutil.MockOsOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		c.Check(name, Equals, "/dev/mapper/control")
		f, err := os.OpenFile(fakeControl, flag, perm)
		c.Assert(err, IsNil)
		return f, nil
	})
	defer restore()

	restore = osutil.MockDmIoctl(func(fd uintptr, command int, data unsafe.Pointer) error {
		return fmt.Errorf("some errno error")
	})
	defer restore()

	_, err := osutil.DmIoctlTableStatus(1, 2)
	c.Assert(err, ErrorMatches, `some errno error`)
}

func (s *dmIoctlSuite) TestDmIoctlOpenError(c *C) {
	restore := osutil.MockOsOpenFile(func(name string, flag int, perm os.FileMode) (*os.File, error) {
		c.Check(name, Equals, "/dev/mapper/control")
		return nil, fmt.Errorf("some error")
	})
	defer restore()

	restore = osutil.MockDmIoctl(func(fd uintptr, command int, data unsafe.Pointer) error {
		c.Error("call unexpected")
		return nil
	})
	defer restore()

	_, err := osutil.DmIoctlTableStatus(1, 2)
	c.Assert(err, ErrorMatches, `cannot open /dev/mapper/control: some error`)
}
