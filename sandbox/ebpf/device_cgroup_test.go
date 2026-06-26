// -*- Mode: Go; indent-tabs-mode: t; tab-width: 4 -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ebpf_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/ebpf"
)

func Test(t *testing.T) { TestingT(t) }

type ebpfSuite struct{}

var _ = Suite(&ebpfSuite{})

func (s *ebpfSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *ebpfSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *ebpfSuite) TestSecurityTagToBPFPath(c *C) {
	c.Check(ebpf.SecurityTagToBPFPath("snap.foo.bar"), Equals,
		filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar"))
	c.Check(ebpf.SecurityTagToBPFPath("snap.my-snap.my-app"), Equals,
		filepath.Join(dirs.SnapBPFFSDir, "snap_my-snap_my-app"))
}

func (s *ebpfSuite) TestDeviceKeyMarshalRoundTrip(c *C) {
	key := ebpf.DeviceKey{Type: 'c', Major: 1, Minor: 3}
	data, err := key.MarshalBinary()
	c.Assert(err, IsNil)
	c.Assert(data, HasLen, ebpf.DeviceKeySize)

	// Verify raw bytes
	c.Check(data[0], Equals, byte('c'))
	e := arch.Endian()
	c.Check(e.Uint32(data[1:5]), Equals, uint32(1))
	c.Check(e.Uint32(data[5:9]), Equals, uint32(3))

	// Round-trip
	var key2 ebpf.DeviceKey
	err = key2.UnmarshalBinary(data)
	c.Assert(err, IsNil)
	c.Check(key2, Equals, key)
}

func (s *ebpfSuite) TestDeviceKeyWildcardMinor(c *C) {
	key := ebpf.DeviceKey{Type: 'b', Major: 8, Minor: 0xFFFFFFFF}
	data, err := key.MarshalBinary()
	c.Assert(err, IsNil)

	var key2 ebpf.DeviceKey
	err = key2.UnmarshalBinary(data)
	c.Assert(err, IsNil)
	c.Check(key2.Minor, Equals, uint32(0xFFFFFFFF))
}

func (s *ebpfSuite) TestDeviceKeyUnmarshalTooShort(c *C) {
	var key2 ebpf.DeviceKey
	err := key2.UnmarshalBinary([]byte{0xff, 0xff})
	c.Assert(err, ErrorMatches, "cannot unmarshal device key: unexpected size 2")
}

func (s *ebpfSuite) TestBpfNameToSecurityTag(c *C) {
	tests := []struct {
		name string
		tag  string
		err  string
	}{
		{"snap_foo_bar", "snap.foo.bar", ""},
		{"snap_my-snap_my-app", "snap.my-snap.my-app", ""},
		// instance key: snap name is "foo_inst", app is "bar"
		{"snap_foo_inst_bar", "snap.foo_inst.bar", ""},

		// bad cases
		{"_", "", `cannot identify security tag from name "_"`},
		{"", "", `cannot identify security tag from name ""`},
		{"snap__", "", `cannot identify security tag from name "snap__"`},
		{"snap_", "", `cannot identify security tag from name "snap_"`},
	}
	for _, t := range tests {
		tag, err := ebpf.BpffsPinnedNameToSecurityTag(t.name)
		if t.err == "" {
			c.Assert(err, IsNil)
			c.Check(tag, Equals, t.tag, Commentf("input: %s", t.name))
		} else {
			c.Check(err, ErrorMatches, t.err)
			c.Check(tag, Equals, "")
		}
	}
}

func (s *ebpfSuite) TestFindActiveDeviceMapsForSnap(c *C) {
	// Create the bpffs directory with some entries
	err := os.MkdirAll(dirs.SnapBPFFSDir, 0755)
	c.Assert(err, IsNil)

	for _, name := range []string{
		"snap_mysnap_app1",
		"snap_mysnap_app2",
		"snap_other_thing",
	} {
		err := os.WriteFile(filepath.Join(dirs.SnapBPFFSDir, name), nil, 0644)
		c.Assert(err, IsNil)
	}

	tags, err := ebpf.FindActiveDeviceMapsForSnap("mysnap")
	c.Assert(err, IsNil)
	c.Check(tags, DeepEquals, []string{"snap.mysnap.app1", "snap.mysnap.app2"})

	// Non-matching snap
	tags, err = ebpf.FindActiveDeviceMapsForSnap("nope")
	c.Assert(err, IsNil)
	c.Check(tags, HasLen, 0)
}

func (s *ebpfSuite) TestFindActiveDeviceMapsForSnapNoDir(c *C) {
	// SnapBPFFSDir doesn't exist — should return nil, nil
	tags, err := ebpf.FindActiveDeviceMapsForSnap("anything")
	c.Assert(err, IsNil)
	c.Check(tags, IsNil)
}

func (s *ebpfSuite) TestSecurityTagToDeviceDenyLogPath(c *C) {
	c.Check(ebpf.SecurityTagToDeviceDenyLogPath("snap.foo.bar"), Equals,
		filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar@denylog"))
}

func (s *ebpfSuite) TestDecodeDeviceDenyEvent(c *C) {
	// Build a 40-byte event payload
	data := make([]byte, 40)
	data[0] = 'c'  // dev_type
	data[1] = 0x06 // access: read|write
	// padding at [2:4]
	e := arch.Endian()
	e.PutUint32(data[4:8], 1)          // major
	e.PutUint32(data[8:12], 7)         // minor
	e.PutUint32(data[12:16], 12345)    // pid
	e.PutUint64(data[16:24], 99999999) // timestamp
	copy(data[24:40], "my-process\x00\x00\x00\x00\x00\x00")

	ev, err := ebpf.DecodeDeviceDenyEvent(data)
	c.Assert(err, IsNil)
	c.Check(ev.DevType, Equals, uint8('c'))
	c.Check(ev.Access, Equals, uint8(0x06))
	c.Check(ev.Major, Equals, uint32(1))
	c.Check(ev.Minor, Equals, uint32(7))
	c.Check(ev.PID, Equals, uint32(12345))
	c.Check(ev.Timestamp, Equals, uint64(99999999))
	c.Check(ev.CommString(), Equals, "my-process")
}

func (s *ebpfSuite) TestDecodeDeviceDenyEventShort(c *C) {
	_, err := ebpf.DecodeDeviceDenyEvent(make([]byte, 10))
	c.Check(err, ErrorMatches, "short event record: 10 bytes, want 40")
}

func (s *ebpfSuite) TestCommString(c *C) {
	ev := &ebpf.DeviceDenyEvent{}

	// Full buffer, no null
	copy(ev.Comm[:], "0123456789abcdef")
	c.Check(ev.CommString(), Equals, "0123456789abcdef")

	// Null terminated early
	copy(ev.Comm[:], "foo\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	c.Check(ev.CommString(), Equals, "foo")

	// Empty
	ev.Comm = [16]byte{}
	c.Check(ev.CommString(), Equals, "")
}

func (s *ebpfSuite) TestAccessString(c *C) {
	ev := &ebpf.DeviceDenyEvent{}

	ev.Access = 1
	c.Check(ev.AccessString(), Equals, "mknod")

	ev.Access = 2
	c.Check(ev.AccessString(), Equals, "read")

	ev.Access = 4
	c.Check(ev.AccessString(), Equals, "write")

	ev.Access = 7
	c.Check(ev.AccessString(), Equals, "mknod,read,write")

	ev.Access = 6
	c.Check(ev.AccessString(), Equals, "read,write")

	ev.Access = 0
	c.Check(ev.AccessString(), Equals, "0x0")
}

func (s *ebpfSuite) TestDiscardPinnedMapsBothExist(c *C) {
	// Create fake pinned files under dirs.SnapBPFFSDir
	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)
	mapPath := filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar")
	ringPath := filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar@denylog")
	c.Assert(os.WriteFile(mapPath, []byte("map"), 0644), IsNil)
	c.Assert(os.WriteFile(ringPath, []byte("ring"), 0644), IsNil)

	var logs []string
	logf := func(format string, a ...any) {
		logs = append(logs, fmt.Sprintf(format, a...))
	}

	err := ebpf.DiscardPinnedMaps("snap.foo.bar", logf)
	c.Assert(err, IsNil)
	c.Check(logs, HasLen, 0)

	// Both files removed
	_, err = os.Stat(mapPath)
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(ringPath)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *ebpfSuite) TestDiscardPinnedMapsOneMissing(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)

	// Only create the map, not the ring buffer
	mapPath := filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar")
	c.Assert(os.WriteFile(mapPath, []byte("map"), 0644), IsNil)

	var logs []string
	logf := func(format string, a ...any) {
		logs = append(logs, fmt.Sprintf(format, a...))
	}

	err := ebpf.DiscardPinnedMaps("snap.foo.bar", logf)
	c.Assert(err, IsNil)
	// One log about missing ring buffer
	c.Check(logs, HasLen, 1)
	c.Check(logs[0], Matches, ".*does not exist.*")

	// Map was removed
	_, err = os.Stat(mapPath)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *ebpfSuite) TestDiscardPinnedMapsBothMissing(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)

	var logs []string
	logf := func(format string, a ...any) {
		logs = append(logs, fmt.Sprintf(format, a...))
	}

	err := ebpf.DiscardPinnedMaps("snap.foo.bar", logf)
	c.Assert(err, IsNil)
	c.Check(logs, HasLen, 2)
}

func (s *ebpfSuite) TestDiscardPinnedMapsPermissionError(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)

	// Create a non-empty subdirectory (os.Remove fails with ENOTEMPTY)
	dirPath := filepath.Join(dirs.SnapBPFFSDir, "snap_foo_bar")
	c.Assert(os.Mkdir(dirPath, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirPath, "child"), []byte("x"), 0644), IsNil)

	var logs []string
	logf := func(format string, a ...any) {
		logs = append(logs, fmt.Sprintf(format, a...))
	}

	err := ebpf.DiscardPinnedMaps("snap.foo.bar", logf)
	c.Check(err, ErrorMatches, "cannot remove.*")
}
