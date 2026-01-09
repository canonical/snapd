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
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type contentTestSuite struct {
	testutil.BaseTest

	dir string

	gadgetRoot string

	mockMountPoint string
	mockMountCalls []struct {
		source, target, fstype string
		flags                  uintptr
	}
	mockUnmountCalls []string

	mockMountErr error
}

var _ = Suite(&contentTestSuite{})

func (s *contentTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })
	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)

	s.mockMountErr = nil
	s.mockMountCalls = nil
	s.mockUnmountCalls = nil

	s.gadgetRoot = c.MkDir()
	err := gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent)
	c.Assert(err, IsNil)

	s.mockMountPoint = c.MkDir()

	restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		s.mockMountCalls = append(s.mockMountCalls, struct {
			source, target, fstype string
			flags                  uintptr
		}{source, target, fstype, flags})
		return s.mockMountErr
	})
	s.AddCleanup(restore)

	restore = install.MockSysUnmount(func(target string, flags int) error {
		s.mockUnmountCalls = append(s.mockUnmountCalls, target)
		return nil
	})
	s.AddCleanup(restore)
}

func mockOnDiskStructureSystemSeed(gadgetRoot string) *gadget.LaidOutStructure {
	return &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "vfat",
			Role:       gadget.SystemSeed,
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "grubx64.efi",
					Target:           "EFI/boot/grubx64.efi",
				},
			},
			YamlIndex: 1000, // to demonstrate we do not use the laid out index
		},
		ResolvedContent: []gadget.ResolvedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					UnresolvedSource: "grubx64.efi",
					Target:           "EFI/boot/grubx64.efi",
				},
				ResolvedSource: filepath.Join(gadgetRoot, "grubx64.efi"),
			},
		},
	}
}

func mockOnDiskStructureSystemData() *gadget.LaidOutStructure {
	return &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
			Role:       gadget.SystemData,
			YamlIndex:  1000, // to demonstrate we do not use the laid out index
		},
	}
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
	content      map[string][]*mockContentChange
	observeErr   error
	expectedRole string
	c            *C
}

func (m *mockWriteObserver) Observe(op gadget.ContentOperation, partRole,
	targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	if m.content == nil {
		m.content = make(map[string][]*mockContentChange)
	}
	m.content[targetRootDir] = append(m.content[targetRootDir],
		&mockContentChange{path: relativeTargetPath, change: data})
	m.c.Check(partRole, Equals, m.expectedRole)
	return gadget.ChangeApply, m.observeErr
}

func (s *contentTestSuite) TestWriteFilesystemContent(c *C) {
	defer dirs.SetRootDir(dirs.GlobalRootDir)

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
			err:        "cannot mount .* at .*: mount error",
		}, {
			mountErr:   nil,
			unmountErr: errors.New("unmount error"),
			err:        "cannot unmount /dev/node2 after writing filesystem content: unmount error",
		}, {
			observeErr: errors.New("observe error"),
			err:        "cannot create filesystem image: cannot write filesystem content of source:grubx64.efi: cannot observe file write: observe error",
		},
	} {
		dirs.SetRootDir(c.MkDir())

		restore := install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
			c.Check(source, Equals, "/dev/node2")
			c.Check(fstype, Equals, "vfat")
			c.Check(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2"))
			return tc.mountErr
		})
		defer restore()

		restore = install.MockSysUnmount(func(target string, flags int) error {
			return tc.unmountErr
		})
		defer restore()

		// copy existing mock
		m := mockOnDiskStructureSystemSeed(s.gadgetRoot)
		obs := &mockWriteObserver{
			c:            c,
			observeErr:   tc.observeErr,
			expectedRole: m.Role(),
		}
		err := install.WriteFilesystemContent(m, nil, "/dev/node2", obs)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}

		if err == nil {
			// the target file system is mounted on a directory named after the structure index
			content, err := os.ReadFile(filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2", "EFI/boot/grubx64.efi"))
			c.Assert(err, IsNil)
			c.Check(string(content), Equals, "grubx64.efi content")
			c.Assert(obs.content, DeepEquals, map[string][]*mockContentChange{
				filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2"): {
					{
						path:   "EFI/boot/grubx64.efi",
						change: &gadget.ContentChange{After: filepath.Join(s.gadgetRoot, "grubx64.efi")},
					},
				},
			})
		}
	}
}

func (s *contentTestSuite) testWriteFilesystemContentDriversTree(c *C, kMntPoint string, modulesComps []install.KernelModulesComponentInfo, isCore bool) {
	defer dirs.SetRootDir(dirs.GlobalRootDir)
	dirs.SetRootDir(c.MkDir())
	restore := osutil.MockMountInfo(``)
	defer restore()

	kMntPoint = filepath.Join(dirs.GlobalRootDir, kMntPoint)

	dataMntPoint := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2")
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		c.Check(source, Equals, "/dev/node2")
		c.Check(fstype, Equals, "ext4")
		c.Check(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2"))
		return nil
	})
	defer restore()

	restore = install.MockSysUnmount(func(target string, flags int) error {
		return nil
	})
	defer restore()

	// copy existing mock
	m := mockOnDiskStructureSystemData()
	obs := &mockWriteObserver{
		c:            c,
		observeErr:   nil,
		expectedRole: m.Role(),
	}
	// mock drivers tree
	treesDir := dirs.SnapKernelDriversTreesDirUnder(dirs.GlobalRootDir)
	modsSubDir := "pc-kernel/111/lib/modules/6.8.0-31-generic"
	modsDir := filepath.Join(treesDir, modsSubDir)
	c.Assert(os.MkdirAll(modsDir, 0o755), IsNil)
	someFile := filepath.Join(modsDir, "modules.alias")
	c.Assert(os.WriteFile(someFile, []byte("blah"), 0o644), IsNil)

	kInfo := &install.KernelSnapInfo{
		Name:             "pc-kernel",
		Revision:         snap.R(111),
		MountPoint:       kMntPoint,
		NeedsDriversTree: true,
		IsCore:           isCore,
		ModulesComps:     modulesComps,
	}

	dataDir := ""
	if isCore {
		dataDir = "system-data/_writable_defaults"
	}
	restore = install.MockKernelEnsureKernelDriversTree(func(kMntPts kernel.MountPoints, compsMntPts []kernel.ModulesCompMountPoints, destDir string, opts *kernel.KernelDriversTreeOptions) (err error) {
		c.Check(kMntPts, Equals,
			kernel.MountPoints{
				Current: kMntPoint,
				Target:  filepath.Join(dirs.SnapMountDir, "/pc-kernel/111")})
		c.Check(destDir, Equals, filepath.Join(dataMntPoint, dataDir,
			"var/lib/snapd/kernel/pc-kernel/111"))
		c.Check(len(compsMntPts), Equals, len(modulesComps))
		if len(modulesComps) > 0 {
			c.Check(compsMntPts, DeepEquals, []kernel.ModulesCompMountPoints{
				{
					LinkName: "kmod1",
					MountPoints: kernel.MountPoints{
						Current: modulesComps[0].MountPoint,
						Target: filepath.Join(dirs.SnapMountDir,
							"/pc-kernel/components/mnt/kmod1/3"),
					},
				},
				{
					LinkName: "kmod2",
					MountPoints: kernel.MountPoints{
						Current: modulesComps[1].MountPoint,
						Target: filepath.Join(dirs.SnapMountDir,
							"/pc-kernel/components/mnt/kmod2/7"),
					},
				},
			})
		}
		return nil
	})
	defer restore()

	err := install.WriteFilesystemContent(m, kInfo, "/dev/node2", obs)
	c.Assert(err, IsNil)

	// Check content of kernel mount unit / links
	cpi := snap.MinimalSnapContainerPlaceInfo("pc-kernel", snap.R(111))
	checkInstallMountUnit(c, filepath.Join(dataMntPoint, dataDir), cpi)

	// now for kernel-modules components
	for _, comp := range modulesComps {
		cpi := snap.MinimalComponentContainerPlaceInfo(comp.Name,
			comp.Revision, "pc-kernel")
		checkInstallMountUnit(c, filepath.Join(dataMntPoint, dataDir), cpi)
	}
}

func checkInstallMountUnit(c *C, dataDir string, cpi snap.ContainerPlaceInfo) {
	whereDir := dirs.StripRootDir(cpi.MountDir())
	unitFileName := systemd.EscapeUnitNamePath(whereDir) + ".mount"
	c.Check(filepath.Join(dataDir, "etc/systemd/system", unitFileName),
		testutil.FileEquals, fmt.Sprintf(
			`[Unit]
Description=%s
After=snapd.mounts-pre.target
Before=snapd.mounts.target
Before=systemd-udevd.service systemd-modules-load.service
Before=usr-lib-modules.mount usr-lib-firmware.mount

[Mount]
What=%s
Where=%s
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
LazyUnmount=yes

[Install]
WantedBy=snapd.mounts.target
WantedBy=multi-user.target
`, cpi.MountDescription(), dirs.StripRootDir(cpi.MountFile()), whereDir))

	for _, target := range []string{"multi-user.target.wants", "snapd.mounts.target.wants"} {
		path, err := os.Readlink(filepath.Join(dataDir,
			"etc/systemd/system", target, unitFileName))
		c.Check(err, IsNil)
		c.Check(path, Equals, filepath.Join(dirs.SnapServicesDir, unitFileName))
	}
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeCore(c *C) {
	isCore := true
	s.testWriteFilesystemContentDriversTree(c,
		filepath.Join(dirs.SnapMountDir, "pc-kernel/111"), nil, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeCoreWithKMods(c *C) {
	isCore := true
	kMods := []install.KernelModulesComponentInfo{{
		Name:       "kmod1",
		Revision:   snap.R(3),
		MountPoint: filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/kmod1/3"),
	}, {
		Name:       "kmod2",
		Revision:   snap.R(7),
		MountPoint: filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/kmod2/7"),
	}}
	s.testWriteFilesystemContentDriversTree(c,
		filepath.Join(dirs.SnapMountDir, "pc-kernel/111"), kMods, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeCoreUnusualMntPt(c *C) {
	isCore := true
	s.testWriteFilesystemContentDriversTree(c, "/somewhere/pc-kernel/111", nil, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeCoreUnusualMntPtWithKMods(c *C) {
	isCore := true
	kMods := []install.KernelModulesComponentInfo{{
		Name:       "kmod1",
		Revision:   snap.R(3),
		MountPoint: "/components/pc-kernel/mnt/kmod1/3",
	}, {
		Name:       "kmod2",
		Revision:   snap.R(7),
		MountPoint: "/components/pc-kernel/mnt/kmod2/7",
	}}
	s.testWriteFilesystemContentDriversTree(c, "/somewhere/pc-kernel/111", kMods, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeHybrid(c *C) {
	isCore := false
	s.testWriteFilesystemContentDriversTree(c,
		filepath.Join(dirs.SnapMountDir, "pc-kernel/111"), nil, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeHybridWithKmods(c *C) {
	isCore := false
	kMods := []install.KernelModulesComponentInfo{{
		Name:       "kmod1",
		Revision:   snap.R(3),
		MountPoint: filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/kmod1/3"),
	}, {
		Name:       "kmod2",
		Revision:   snap.R(7),
		MountPoint: filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/kmod2/7"),
	}}
	s.testWriteFilesystemContentDriversTree(c,
		filepath.Join(dirs.SnapMountDir, "pc-kernel/111"), kMods, isCore)
}

func (s *contentTestSuite) TestWriteFilesystemContentDriversTreeIdempotent(c *C) {
	defer dirs.SetRootDir(dirs.GlobalRootDir)
	dirs.SetRootDir(c.MkDir())
	restore := osutil.MockMountInfo("")
	defer restore()

	kMntPoint := filepath.Join(dirs.SnapMountDir, "pc-kernel/111")
	kMntPoint = filepath.Join(dirs.GlobalRootDir, kMntPoint)

	dataMntPoint := filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2")
	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		c.Check(source, Equals, "/dev/node2")
		c.Check(fstype, Equals, "ext4")
		c.Check(target, Equals, filepath.Join(dirs.SnapRunDir, "gadget-install/dev-node2"))
		return nil
	})
	defer restore()

	restore = install.MockSysUnmount(func(target string, flags int) error {
		return nil
	})
	defer restore()

	m := mockOnDiskStructureSystemData()
	obs := &mockWriteObserver{
		c:            c,
		observeErr:   nil,
		expectedRole: m.Role(),
	}

	treesDir := dirs.SnapKernelDriversTreesDirUnder(dirs.GlobalRootDir)
	modsSubDir := "pc-kernel/111/lib/modules/6.8.0-31-generic"
	modsDir := filepath.Join(treesDir, modsSubDir)
	c.Assert(os.MkdirAll(modsDir, 0o755), IsNil)
	someFile := filepath.Join(modsDir, "modules.alias")
	c.Assert(os.WriteFile(someFile, []byte("blah"), 0o644), IsNil)

	kMods := []install.KernelModulesComponentInfo{{
		Name:       "kmod1",
		Revision:   snap.R(3),
		MountPoint: filepath.Join(dirs.SnapMountDir, "pc-kernel/components/mnt/kmod1/3"),
	}}
	kInfo := &install.KernelSnapInfo{
		Name:             "pc-kernel",
		Revision:         snap.R(111),
		MountPoint:       kMntPoint,
		NeedsDriversTree: true,
		IsCore:           false,
		ModulesComps:     kMods,
	}

	var calls int
	restore = install.MockKernelEnsureKernelDriversTree(func(kMntPts kernel.MountPoints, compsMntPts []kernel.ModulesCompMountPoints, destDir string, opts *kernel.KernelDriversTreeOptions) (err error) {
		calls++
		c.Check(kMntPts, Equals,
			kernel.MountPoints{
				Current: kMntPoint,
				Target:  filepath.Join(dirs.SnapMountDir, "/pc-kernel/111")})
		c.Check(destDir, Equals, filepath.Join(dataMntPoint,
			"var/lib/snapd/kernel/pc-kernel/111"))
		c.Check(compsMntPts, DeepEquals, []kernel.ModulesCompMountPoints{
			{
				LinkName: "kmod1",
				MountPoints: kernel.MountPoints{
					Current: kMods[0].MountPoint,
					Target: filepath.Join(dirs.SnapMountDir,
						"/pc-kernel/components/mnt/kmod1/3"),
				},
			},
		})
		return nil
	})
	defer restore()

	err := install.WriteFilesystemContent(m, kInfo, "/dev/node2", obs)
	c.Assert(err, IsNil)
	err = install.WriteFilesystemContent(m, kInfo, "/dev/node2", obs)
	c.Assert(err, IsNil)
	c.Assert(calls, Equals, 2)

	cpi := snap.MinimalSnapContainerPlaceInfo("pc-kernel", snap.R(111))
	checkInstallMountUnit(c, dataMntPoint, cpi)
	for _, comp := range kMods {
		cpi := snap.MinimalComponentContainerPlaceInfo(comp.Name,
			comp.Revision, "pc-kernel")
		checkInstallMountUnit(c, dataMntPoint, cpi)
	}
}

func (s *contentTestSuite) TestWriteFilesystemContentUnmountErrHandling(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir(dirs.GlobalRootDir)

	log, restore := logger.MockLogger()
	defer restore()

	type unmountArgs struct {
		target string
		flags  int
	}

	restore = install.MockSysMount(func(source, target, fstype string, flags uintptr, data string) error {
		return nil
	})
	defer restore()

	// copy existing mock
	m := mockOnDiskStructureSystemSeed(s.gadgetRoot)
	obs := &mockWriteObserver{
		c:            c,
		observeErr:   nil,
		expectedRole: m.Role(),
	}

	for _, tc := range []struct {
		unmountErr     error
		lazyUnmountErr error

		expectedErr string
	}{
		{
			nil,
			nil,
			"",
		}, {
			errors.New("umount error"),
			nil,
			"", // no error as lazy unmount succeeded
		}, {
			errors.New("umount error"),
			errors.New("lazy umount err"),
			`cannot unmount /dev/node2 after writing filesystem content: lazy umount err`},
	} {
		log.Reset()

		var unmountCalls []unmountArgs
		restore = install.MockSysUnmount(func(target string, flags int) error {
			unmountCalls = append(unmountCalls, unmountArgs{target, flags})
			switch flags {
			case 0:
				return tc.unmountErr
			case syscall.MNT_DETACH:
				return tc.lazyUnmountErr
			default:
				return fmt.Errorf("unexpected mount flag %v", flags)
			}
		})
		defer restore()

		err := install.WriteFilesystemContent(m, nil, "/dev/node2", obs)
		if tc.expectedErr == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.expectedErr)
		}
		if tc.unmountErr == nil {
			c.Check(unmountCalls, HasLen, 1)
			c.Check(unmountCalls[0].flags, Equals, 0)
			c.Check(log.String(), Equals, "")
		} else {
			c.Check(unmountCalls, HasLen, 2)
			c.Check(unmountCalls[0].flags, Equals, 0)
			c.Check(unmountCalls[1].flags, Equals, syscall.MNT_DETACH)
			c.Check(log.String(), Matches, `(?sm).* cannot unmount /.*/run/snapd/gadget-install/dev-node2 after writing filesystem content: umount error \(trying lazy unmount next\)`)
		}
	}
}

func (s *contentTestSuite) TestMakeFilesystem(c *C) {
	mockUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer mockUdevadm.Restore()

	restore := install.MockMkfsMake(func(typ, img, label string, devSize, sectorSize quantity.Size) error {
		c.Assert(typ, Equals, "ext4")
		c.Assert(img, Equals, "/dev/node3")
		c.Assert(label, Equals, "ubuntu-data")
		c.Assert(devSize, Equals, mockOnDiskStructureWritable.Size)
		c.Assert(sectorSize, Equals, quantity.Size(512))
		return nil
	})
	defer restore()

	err := install.MakeFilesystem(install.MkfsParams{
		Type:       mockOnDiskStructureWritable.PartitionFSType,
		Device:     mockOnDiskStructureWritable.Node,
		Label:      mockOnDiskStructureWritable.PartitionFSLabel,
		Size:       mockOnDiskStructureWritable.Size,
		SectorSize: quantity.Size(512),
	})
	c.Assert(err, IsNil)

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--settle", "/dev/node3"},
	})
}

func (s *contentTestSuite) TestMakeFilesystemRealMkfs(c *C) {
	mockUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer mockUdevadm.Restore()

	mockMkfsExt4 := testutil.MockCommand(c, "mkfs.ext4", "")
	defer mockMkfsExt4.Restore()

	err := install.MakeFilesystem(install.MkfsParams{
		Type:       mockOnDiskStructureWritable.PartitionFSType,
		Device:     mockOnDiskStructureWritable.Node,
		Label:      mockOnDiskStructureWritable.PartitionFSLabel,
		Size:       mockOnDiskStructureWritable.Size,
		SectorSize: quantity.Size(512),
	})
	c.Assert(err, IsNil)

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "trigger", "--settle", "/dev/node3"},
	})

	c.Assert(mockMkfsExt4.Calls(), DeepEquals, [][]string{
		{"mkfs.ext4", "-L", "ubuntu-data", "/dev/node3"},
	})
}

func (s *contentTestSuite) TestMountFilesystem(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	// mount a filesystem...
	err := install.MountFilesystem("/dev/node2", "vfat", filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed"), install.MntfsParams{})
	c.Assert(err, IsNil)

	// ...and check if it was mounted at the right mount point
	c.Check(s.mockMountCalls, HasLen, 1)
	c.Check(s.mockMountCalls, DeepEquals, []struct {
		source, target, fstype string
		flags                  uintptr
	}{
		{"/dev/node2", boot.InitramfsUbuntuSeedDir, "vfat", 0},
	})

	// try again with mocked error
	s.mockMountErr = fmt.Errorf("mock mount error")
	err = install.MountFilesystem("/dev/node2", "vfat", filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed"), install.MntfsParams{})
	c.Assert(err, ErrorMatches, `cannot mount filesystem "/dev/node2" at ".*/run/mnt/ubuntu-seed": mock mount error`)
}

func (s *contentTestSuite) TestMountFilesystemOptions(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	tests := []struct {
		params        install.MntfsParams
		expectedFlags uintptr
	}{
		{install.MntfsParams{}, 0},
		{install.MntfsParams{NoExec: true}, syscall.MS_NOEXEC},
		{install.MntfsParams{NoDev: true}, syscall.MS_NODEV},
		{install.MntfsParams{NoSuid: true}, syscall.MS_NOSUID},
	}

	for _, t := range tests {
		// reset calls
		s.mockMountCalls = nil

		err := install.MountFilesystem("/dev/node2", "vfat", filepath.Join(boot.InitramfsRunMntDir, "ubuntu-seed"), t.params)
		c.Assert(err, IsNil)

		// .. verify flags
		c.Check(s.mockMountCalls, HasLen, 1)
		c.Check(s.mockMountCalls, DeepEquals, []struct {
			source, target, fstype string
			flags                  uintptr
		}{
			{"/dev/node2", boot.InitramfsUbuntuSeedDir, "vfat", t.expectedFlags},
		})
	}
}
