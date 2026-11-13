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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/sandbox/ebpf"
	"github.com/snapcore/snapd/snap/naming"
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

	restoreFind := cgroup.MockFindActiveDeviceMapTagsForSnap(func(snapName string) ([]string, error) {
		c.Check(snapName, Equals, "mysnap")
		return []string{"snap.mysnap.app1", "snap.mysnap.app2"}, nil
	})
	defer restoreFind()

	tags, err := cgroup.FindActiveDeviceMediationForSnap("mysnap")
	c.Assert(err, IsNil)
	c.Check(tags, DeepEquals, []string{"snap.mysnap.app1", "snap.mysnap.app2"})
}

func (s *devicesSuite) TestRemoveDeviceCgroupV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app1"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app2"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.other.app1"), 0755), IsNil)
	// A regular file should be ignored by the dir filter.
	c.Assert(os.WriteFile(filepath.Join(cgroupDir, "snap.mysnap.ignored"), nil, 0644), IsNil)

	err := cgroup.RemoveDeviceCgroupV1(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(filepath.Join(cgroupDir, "snap.mysnap.app1"), testutil.FileAbsent)
	c.Check(filepath.Join(cgroupDir, "snap.mysnap.app2"), testutil.FileAbsent)
	// Other snaps are untouched.
	c.Check(filepath.Join(cgroupDir, "snap.other.app1"), testutil.FilePresent)
	// Non-directory entries are untouched.
	c.Check(filepath.Join(cgroupDir, "snap.mysnap.ignored"), testutil.FilePresent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV1NoDir(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	err := cgroup.RemoveDeviceCgroupV1(naming.InstanceName("anything"))
	c.Assert(err, IsNil)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV1EmptyDir(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	c.Assert(os.MkdirAll(cgroupDir, 0755), IsNil)
	err := cgroup.RemoveDeviceCgroupV1(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
}

func (s *devicesSuite) TestRemoveDeviceCgroupDispatchV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	c.Assert(os.MkdirAll(filepath.Join(cgroupDir, "snap.mysnap.app1"), 0755), IsNil)

	err := cgroup.RemoveDeviceCgroup(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(filepath.Join(cgroupDir, "snap.mysnap.app1"), testutil.FileAbsent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV1InstanceKey(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	instDir := filepath.Join(cgroupDir, "snap.mysnap_inst.app1")
	baseDir := filepath.Join(cgroupDir, "snap.mysnap.app1")
	c.Assert(os.MkdirAll(instDir, 0755), IsNil)
	c.Assert(os.MkdirAll(baseDir, 0755), IsNil)

	// The instance-keyed prefix is "snap.mysnap_inst." which must not
	// leak into the base snap "snap.mysnap.app1" entry.
	err := cgroup.RemoveDeviceCgroup(naming.InstanceName("mysnap_inst"))
	c.Assert(err, IsNil)
	c.Check(instDir, testutil.FileAbsent)
	c.Check(baseDir, testutil.FilePresent)

	// The reverse direction: removing the base instance must not
	// over-match the instance-keyed entry.
	c.Assert(os.MkdirAll(instDir, 0755), IsNil)
	err = cgroup.RemoveDeviceCgroup(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(baseDir, testutil.FileAbsent)
	c.Check(instDir, testutil.FilePresent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV1RemoveError(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupDir := filepath.Join(s.rootDir, "/sys/fs/cgroup/devices")
	app1 := filepath.Join(cgroupDir, "snap.mysnap.app1")
	app2 := filepath.Join(cgroupDir, "snap.mysnap.app2")
	// Simulate that app1 has a child cgroup so os.Remove fails
	c.Assert(os.MkdirAll(filepath.Join(app1, "inner"), 0755), IsNil)
	c.Assert(os.MkdirAll(app2, 0755), IsNil)

	err := cgroup.RemoveDeviceCgroupV1(naming.InstanceName("mysnap"))
	c.Assert(err, NotNil)
	// The failure is not busy: EBUSY cannot be produced on a plain
	// filesystem
	c.Check(err, ErrorMatches, "remove .*/sys/fs/cgroup/devices/snap.mysnap.app1: directory not empty")
	// Best-effort: the empty second entry was still removed.
	c.Check(app2, testutil.FileAbsent)
	c.Check(app1, testutil.FilePresent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	tags := []string{"snap.mysnap.app1", "snap.mysnap.app2"}
	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)
	for _, tag := range tags {
		c.Assert(os.WriteFile(ebpf.SecurityTagToBPFPath(tag), nil, 0644), IsNil)
	}

	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	for _, tag := range tags {
		c.Check(ebpf.SecurityTagToBPFPath(tag), testutil.FileAbsent)
	}
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2NoTags(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	// No SnapBPFFSDir created
	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2EnumerateError(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	mockErr := errors.New("mock error")
	restoreFind := cgroup.MockFindActiveDeviceMapPathsForSnap(func(snapName string) ([]string, error) {
		c.Check(snapName, Equals, "mysnap")
		return nil, mockErr
	})
	defer restoreFind()

	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, mockErr), Equals, true)
	c.Check(err, ErrorMatches, `cannot enumerate device maps for snap "mysnap": mock error`)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2MissingPin(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	restoreFind := cgroup.MockFindActiveDeviceMapPathsForSnap(func(string) ([]string, error) {
		return []string{filepath.Join(dirs.SnapBPFFSDir, "snap_mysnap_app1")}, nil
	})
	defer restoreFind()

	// No pin file created; ENOENT is skipped, not an error.
	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2ComponentHook(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)
	// Pin file names keep the literal '+' (SecurityTagToBPFPath replaces
	// dots only), so component hook pins match the +<component>_hook_<hook>
	// suffix shape.
	appPin := ebpf.SecurityTagToBPFPath("snap.mysnap.app1")
	compHookPin := ebpf.SecurityTagToBPFPath("snap.mysnap+comp.hook.install")
	instCompHookPin := ebpf.SecurityTagToBPFPath("snap.mysnap_inst+comp.hook.install")
	c.Assert(os.WriteFile(appPin, nil, 0644), IsNil)
	c.Assert(os.WriteFile(compHookPin, nil, 0644), IsNil)
	c.Assert(os.WriteFile(instCompHookPin, nil, 0644), IsNil)

	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(appPin, testutil.FileAbsent)
	c.Check(compHookPin, testutil.FileAbsent)
	// The parallel instance's component hook pin is untouched.
	c.Check(instCompHookPin, testutil.FilePresent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupV2InstanceKey(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)
	// Create pins for both the base snap and a parallel instance; only the
	// instance-keyed pin must be removed.
	c.Assert(os.WriteFile(ebpf.SecurityTagToBPFPath("snap.mysnap.app1"), nil, 0644), IsNil)
	c.Assert(os.WriteFile(ebpf.SecurityTagToBPFPath("snap.mysnap_inst.app1"), nil, 0644), IsNil)

	err := cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap_inst"))
	c.Assert(err, IsNil)
	c.Check(ebpf.SecurityTagToBPFPath("snap.mysnap_inst.app1"), testutil.FileAbsent)
	c.Check(ebpf.SecurityTagToBPFPath("snap.mysnap.app1"), testutil.FilePresent)

	// The reverse direction: removing the base instance must not
	// over-match the parallel instance's pin — the bare "snap_mysnap_"
	// prefix would also match snap_mysnap_inst_app1, only the suffix
	// grammar in the enumeration prevents it.
	c.Assert(os.WriteFile(ebpf.SecurityTagToBPFPath("snap.mysnap_inst.app1"), nil, 0644), IsNil)
	err = cgroup.RemoveDeviceCgroupV2(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(ebpf.SecurityTagToBPFPath("snap.mysnap.app1"), testutil.FileAbsent)
	c.Check(ebpf.SecurityTagToBPFPath("snap.mysnap_inst.app1"), testutil.FilePresent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupDispatchV2(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	c.Assert(os.MkdirAll(dirs.SnapBPFFSDir, 0755), IsNil)
	c.Assert(os.WriteFile(ebpf.SecurityTagToBPFPath("snap.mysnap.app1"), nil, 0644), IsNil)

	err := cgroup.RemoveDeviceCgroup(naming.InstanceName("mysnap"))
	c.Assert(err, IsNil)
	c.Check(ebpf.SecurityTagToBPFPath("snap.mysnap.app1"), testutil.FileAbsent)
}

func (s *devicesSuite) TestRemoveDeviceCgroupVersionError(c *C) {
	mockErr := errors.New("mock error")
	restore := cgroup.MockVersion(cgroup.Unknown, mockErr)
	defer restore()

	err := cgroup.RemoveDeviceCgroup(naming.InstanceName("mysnap"))
	c.Assert(err, NotNil)
	c.Check(errors.Is(err, mockErr), Equals, true)
	c.Check(err, ErrorMatches, "cannot determine cgroup version: mock error")
}
