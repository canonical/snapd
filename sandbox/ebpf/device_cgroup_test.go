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
	}{
		{"snap_foo_bar", "snap.foo.bar"},
		{"snap_my-snap_my-app", "snap.my-snap.my-app"},
		// instance key: snap name is "foo_inst", app is "bar"
		{"snap_foo_inst_bar", "snap.foo_inst.bar"},
	}
	for _, t := range tests {
		c.Check(ebpf.BpfNameToSecurityTag(t.name), Equals, t.tag, Commentf("input: %s", t.name))
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
