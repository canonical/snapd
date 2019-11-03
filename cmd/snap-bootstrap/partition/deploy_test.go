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
package partition_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

type deploySuite struct {
	testutil.BaseTest

	gadgetRoot string

	mockMountPoint   string
	mockMountCalls   []struct{ source, target, fstype string }
	mockUnmountCalls []string

	mockMountErr error
}

var _ = Suite(&deploySuite{})

func (s *deploySuite) SetUpTest(c *C) {
	s.gadgetRoot = c.MkDir()
	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)

	s.mockMountPoint = c.MkDir()
	restore := partition.MockDeployMountpoint(s.mockMountPoint)
	s.AddCleanup(restore)

	restore = partition.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		s.mockMountCalls = append(s.mockMountCalls, struct{ source, target, fstype string }{source, target, fstype})
		return s.mockMountErr
	})
	s.AddCleanup(restore)

	restore = partition.MockSysUnmount(func(target string, flags int) error {
		s.mockUnmountCalls = append(s.mockUnmountCalls, target)
		return nil
	})
	s.AddCleanup(restore)
}

func (s *deploySuite) TestDeployMountedContentErr(c *C) {
	s.mockMountErr = fmt.Errorf("boom")

	node2MountPoint := filepath.Join(s.mockMountPoint, "2")
	err := partition.DeployContent(mockDeviceStructureSystemSeed, s.gadgetRoot)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot mount filesystem "/dev/node2" to %q: boom`, node2MountPoint))
}

func (s *deploySuite) TestDeployMountedContent(c *C) {
	err := partition.DeployContent(mockDeviceStructureSystemSeed, s.gadgetRoot)
	c.Assert(err, IsNil)

	node2MountPoint := filepath.Join(s.mockMountPoint, "2")
	c.Check(s.mockMountCalls, DeepEquals, []struct{ source, target, fstype string }{
		{"/dev/node2", node2MountPoint, "vfat"},
	})
	c.Check(s.mockUnmountCalls, DeepEquals, []string{node2MountPoint})

	c.Check(filepath.Join(node2MountPoint, "EFI/boot/grubx64.efi"), testutil.FilePresent)
	c.Assert(err, IsNil)
}

func (s *deploySuite) TestDeployRawContent(c *C) {
	mockNode := filepath.Join(c.MkDir(), "mock-node")
	err := ioutil.WriteFile(mockNode, nil, 0644)
	c.Assert(err, IsNil)

	// copy existing mock
	m := mockDeviceStructureBiosBoot
	m.Node = mockNode
	m.LaidOutContent = []gadget.LaidOutContent{
		{
			VolumeContent: &gadget.VolumeContent{
				Image: "pc-core.img",
			},
			StartOffset: 2,
			Size:        gadget.Size(len("pc-core.img content")),
		},
	}

	err = partition.DeployContent(m, s.gadgetRoot)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(m.Node)
	c.Assert(err, IsNil)
	// note the 2 zero byte start offset
	c.Check(string(content), Equals, "\x00\x00pc-core.img content")
}
