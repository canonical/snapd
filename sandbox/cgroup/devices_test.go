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
package cgroup_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/ebpf"
	"github.com/snapcore/snapd/testutil"
)

type devicesSuite struct {
	testutil.BaseTest
	rootDir string
	log     *bytes.Buffer
}

var _ = Suite(&devicesSuite{})

func (s *devicesSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	l, r := logger.MockDebugLogger()
	s.AddCleanup(r)
	s.log = l
}

// V1 tests

func (s *devicesSuite) TestFindSecurityTagsV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app1"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app2"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.other.thing"), 0755), IsNil)
	// A regular file should be ignored
	c.Assert(os.WriteFile(filepath.Join(cgroupDir, "snap.mysnap.ignored"), nil, 0644), IsNil)

	tags, err := cgroup.FindSecurityTagsV1("mysnap")
	c.Assert(err, IsNil)
	c.Check(tags, DeepEquals, []string{"snap.mysnap.app1", "snap.mysnap.app2"})
}

func (s *devicesSuite) TestFindSecurityTagsV1NoDir(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	tags, err := cgroup.FindSecurityTagsV1("anything")
	c.Assert(err, IsNil)
	c.Check(tags, IsNil)
}

func (s *devicesSuite) TestCollectDevicesV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	tag := "snap.mysnap.app1"
	devicesDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices", tag)
	c.Assert(os.MkdirAll(devicesDir, 0755), IsNil)

	content := `c 1:3 rwm
b 8:* rw
a *:* rwm

c 5:0 r

# malformed
z -1 m
z 1:1a f
z
b 12:z rwm
`
	c.Assert(os.WriteFile(filepath.Join(devicesDir, "devices.list"), []byte(content), 0644), IsNil)

	entries, err := cgroup.CollectDevicesV1(tag)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 4)
	c.Check(entries[0], Equals, cgroup.DeviceEntry{DevType: 'c', Major: 1, Minor: 3, Access: "rwm"})
	c.Check(entries[1], Equals, cgroup.DeviceEntry{DevType: 'b', Major: 8, Minor: cgroup.AccessAny, Access: "rw"})
	c.Check(entries[2], Equals, cgroup.DeviceEntry{DevType: 'a', Major: cgroup.AccessAny, Minor: cgroup.AccessAny, Access: "rwm"})
	c.Check(entries[3], Equals, cgroup.DeviceEntry{DevType: 'c', Major: 5, Minor: 0, Access: "r"})

	l := s.log.String()
	c.Check(l, testutil.Contains, "malformed minor number:")
	c.Check(l, testutil.Contains, "unexpected number of fields: 2")
	c.Check(l, testutil.Contains, "unexpected format of major:minor")
}

func (s *devicesSuite) TestCollectDevicesV1MissingFile(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	_, err := cgroup.CollectDevicesV1("snap.noexist.app")
	c.Assert(err, ErrorMatches, "cannot open .*/devices.list:.*")
}

func (s *devicesSuite) TestListMediatedDevicesV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	tag := "snap.mysnap.app1"
	devicesDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices", tag)
	c.Assert(os.MkdirAll(devicesDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(devicesDir, "devices.list"), []byte("c 1:3 rwm\n"), 0644), IsNil)

	entries, err := cgroup.ListMediatedDevicesForSecurityTag(tag)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 1)
	c.Check(entries[0], Equals, cgroup.DeviceEntry{DevType: 'c', Major: 1, Minor: 3, Access: "rwm"})
}

func (s *devicesSuite) TestFindActiveDeviceMediationV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app1"), 0755), IsNil)

	tags, err := cgroup.FindActiveDeviceMediationForSnap("mysnap")
	c.Assert(err, IsNil)
	c.Check(tags, DeepEquals, []string{"snap.mysnap.app1"})
}

// V2 tests (mocked)

type fakeDeviceMapAccessor struct {
	keys       []ebpf.DeviceKey
	iterateErr error
}

func (f *fakeDeviceMapAccessor) Iterate(fn func(ebpf.DeviceKey) error) error {
	if f.iterateErr != nil {
		return f.iterateErr
	}

	for _, k := range f.keys {
		if err := fn(k); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeDeviceMapAccessor) Close() error {
	return nil
}

func (s *devicesSuite) TestCollectDevicesV2(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	fakeMap := &fakeDeviceMapAccessor{
		keys: []ebpf.DeviceKey{
			{Type: 'c', Major: 1, Minor: 3},
			{Type: 'b', Major: 8, Minor: 0},
		},
	}
	restore = cgroup.MockLoadDeviceMap(func(tag string) (cgroup.DeviceMapAccessor, error) {
		c.Check(tag, Equals, "snap.mysnap.app1")
		return fakeMap, nil
	})
	defer restore()

	entries, err := cgroup.ListMediatedDevicesForSecurityTag("snap.mysnap.app1")
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 2)
	c.Check(entries[0], Equals, cgroup.DeviceEntry{DevType: 'c', Major: 1, Minor: 3, Access: "rwm"})
	c.Check(entries[1], Equals, cgroup.DeviceEntry{DevType: 'b', Major: 8, Minor: 0, Access: "rwm"})

	// inject an error
	fakeMap.iterateErr = fmt.Errorf("mock error")
	entries, err = cgroup.ListMediatedDevicesForSecurityTag("snap.mysnap.app1")
	c.Assert(err, ErrorMatches, "cannot iterate device map: mock error")
	c.Assert(entries, IsNil)
}

func (s *devicesSuite) TestCollectDevicesV2Error(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	restoreLoad := cgroup.MockLoadDeviceMap(func(tag string) (cgroup.DeviceMapAccessor, error) {
		return nil, fmt.Errorf("mock error")
	})
	defer restoreLoad()

	_, err := cgroup.ListMediatedDevicesForSecurityTag("snap.mysnap.app1")
	c.Assert(err, ErrorMatches, "cannot open device map: mock error")
}

func (s *devicesSuite) TestFindActiveDeviceMediationV2(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	restoreFind := cgroup.MockFindDeviceMapsForSnap(func(snapName string) ([]string, error) {
		c.Check(snapName, Equals, "mysnap")
		return []string{"snap.mysnap.app1", "snap.mysnap.app2"}, nil
	})
	defer restoreFind()

	tags, err := cgroup.FindActiveDeviceMediationForSnap("mysnap")
	c.Assert(err, IsNil)
	c.Check(tags, DeepEquals, []string{"snap.mysnap.app1", "snap.mysnap.app2"})
}
