// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/testutil"
)

type contentTestSuite struct {
	testutil.BaseTest

	dir string

	gadgetRoot string

	mockMountPoint   string
	mockMountCalls   []struct{ source, target, fstype string }
	mockUnmountCalls []string

	mockMountErr error
}

var _ = Suite(&contentTestSuite{})

func (s *contentTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()

	s.mockMountErr = nil
	s.mockMountCalls = nil
	s.mockUnmountCalls = nil

	s.gadgetRoot = c.MkDir()
	err := makeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)

	s.mockMountPoint = c.MkDir()
	restore := install.MockContentMountpoint(s.mockMountPoint)
	s.AddCleanup(restore)

	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		s.mockMountCalls = append(s.mockMountCalls, struct{ source, target, fstype string }{source, target, fstype})
		return s.mockMountErr
	})
	s.AddCleanup(restore)

	restore = install.MockSysUnmount(func(target string, flags int) error {
		s.mockUnmountCalls = append(s.mockUnmountCalls, target)
		return nil
	})
	s.AddCleanup(restore)
}

var mockOnDiskStructureBiosBoot = gadget.OnDiskStructure{
	Node: "/dev/node1",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "BIOS Boot",
			Size: 1 * 1024 * 1024,
			Type: "DA,21686148-6449-6E6F-744E-656564454649",
			Content: []gadget.VolumeContent{
				{
					Image: "pc-core.img",
				},
			},
		},
		StartOffset: 0,
		Index:       1,
	},
}

var mockOnDiskStructureSystemSeed = gadget.OnDiskStructure{
	Node: "/dev/node2",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "Recovery",
			Size:       1258291200,
			Type:       "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Role:       "system-seed",
			Label:      "ubuntu-seed",
			Filesystem: "vfat",
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "grubx64.efi",
					Target:           "EFI/boot/grubx64.efi",
				},
			},
		},
		StartOffset: 2097152,
		Index:       2,
	},
}

func makeMockGadget(gadgetRoot, gadgetContent string) error {
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), []byte("pc-boot.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-core.img"), []byte("pc-core.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), []byte("grubx64.efi content"), 0644); err != nil {
		return err
	}

	return nil
}

const gadgetContent = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

type mockContentChange struct {
	path   string
	change *gadget.ContentChange
}

type mockWriteObserver struct {
	content        map[string][]*mockContentChange
	observeErr     error
	expectedStruct *gadget.LaidOutStructure
	c              *C
}

func (m *mockWriteObserver) Observe(op gadget.ContentOperation, sourceStruct *gadget.LaidOutStructure,
	targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	if m.content == nil {
		m.content = make(map[string][]*mockContentChange)
	}
	m.content[targetRootDir] = append(m.content[targetRootDir],
		&mockContentChange{path: relativeTargetPath, change: data})
	m.c.Assert(sourceStruct, NotNil)
	m.c.Check(sourceStruct, DeepEquals, m.expectedStruct)
	return gadget.ChangeApply, m.observeErr
}

func (s *contentTestSuite) TestWriteFilesystemContent(c *C) {
	for _, tc := range []struct {
		mountErr   error
		unmountErr error
		observeErr error
		err        string
	}{
		{
			mountErr:   nil,
			unmountErr: nil,
			err:        "",
		}, {
			mountErr:   errors.New("mount error"),
			unmountErr: nil,
			err:        "cannot mount filesystem .*: mount error",
		}, {
			mountErr:   nil,
			unmountErr: errors.New("unmount error"),
			err:        "unmount error",
		}, {
			observeErr: errors.New("observe error"),
			err:        "cannot create filesystem image: cannot write filesystem content of source:grubx64.efi: cannot observe file write: observe error",
		},
	} {
		mockMountpoint := c.MkDir()

		restore := install.MockContentMountpoint(mockMountpoint)
		defer restore()

		restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
			return tc.mountErr
		})
		defer restore()

		restore = install.MockSysUnmount(func(target string, flags int) error {
			return tc.unmountErr
		})
		defer restore()

		// copy existing mock
		m := mockOnDiskStructureSystemSeed
		m.LaidOutContent = []gadget.LaidOutContent{
			{
				VolumeContent: &gadget.VolumeContent{
					UnresolvedSource: "grubx64.efi",
					Target:           "EFI/boot/grubx64.efi",
				},
			},
		}
		obs := &mockWriteObserver{
			c:              c,
			observeErr:     tc.observeErr,
			expectedStruct: &m.LaidOutStructure,
		}
		err := install.WriteContent(&m, s.gadgetRoot, obs)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}

		if err == nil {
			// the target file system is mounted on a directory named after the structure index
			content, err := ioutil.ReadFile(filepath.Join(mockMountpoint, "2", "EFI/boot/grubx64.efi"))
			c.Assert(err, IsNil)
			c.Check(string(content), Equals, "grubx64.efi content")
			c.Assert(obs.content, DeepEquals, map[string][]*mockContentChange{
				filepath.Join(mockMountpoint, "2"): {
					{
						path:   "EFI/boot/grubx64.efi",
						change: &gadget.ContentChange{After: filepath.Join(s.gadgetRoot, "grubx64.efi")},
					},
				},
			})
		}
	}
}

func (s *contentTestSuite) TestWriteRawContent(c *C) {
	mockNode := filepath.Join(s.dir, "mock-node")
	err := ioutil.WriteFile(mockNode, nil, 0644)
	c.Assert(err, IsNil)

	// copy existing mock
	m := mockOnDiskStructureBiosBoot
	m.Node = mockNode
	m.LaidOutContent = []gadget.LaidOutContent{
		{
			VolumeContent: &gadget.VolumeContent{
				Image: "pc-core.img",
			},
			StartOffset: 2,
			Size:        quantity.Size(len("pc-core.img content")),
		},
	}

	err = install.WriteContent(&m, s.gadgetRoot, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(m.Node)
	c.Assert(err, IsNil)
	// note the 2 zero byte start offset
	c.Check(string(content), Equals, "\x00\x00pc-core.img content")
}

func (s *contentTestSuite) TestMountFilesystem(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// mounting will only happen for devices with a label
	mockOnDiskStructureBiosBoot.Label = "bios-boot"
	defer func() { mockOnDiskStructureBiosBoot.Label = "" }()

	err := install.MountFilesystem(&mockOnDiskStructureBiosBoot, boot.InitramfsRunMntDir)
	c.Assert(err, ErrorMatches, "cannot mount a partition with no filesystem")

	// mount a filesystem...
	err = install.MountFilesystem(&mockOnDiskStructureSystemSeed, boot.InitramfsRunMntDir)
	c.Assert(err, IsNil)

	// ...and check if it was mounted at the right mount point
	c.Check(s.mockMountCalls, HasLen, 1)
	c.Check(s.mockMountCalls, DeepEquals, []struct{ source, target, fstype string }{
		{"/dev/node2", boot.InitramfsUbuntuSeedDir, "vfat"},
	})

	// now try to mount a filesystem with no label
	mockOnDiskStructureSystemSeed.Label = ""
	defer func() { mockOnDiskStructureSystemSeed.Label = "ubuntu-seed" }()

	err = install.MountFilesystem(&mockOnDiskStructureSystemSeed, boot.InitramfsRunMntDir)
	c.Assert(err, ErrorMatches, "cannot mount a filesystem with no label")
}
