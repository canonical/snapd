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

package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var brandPrivKey, _ = assertstest.GenerateKey(752)

type initramfsMountsSuite struct {
	testutil.BaseTest

	// makes available a bunch of helper (like MakeAssertedSnap)
	*seedtest.TestingSeed20

	Stdout *bytes.Buffer

	seedDir  string
	runMnt   string
	sysLabel string
}

var _ = Suite(&initramfsMountsSuite{})

func (s *initramfsMountsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.Stdout = bytes.NewBuffer(nil)
	restore := main.MockStdout(s.Stdout)
	s.AddCleanup(restore)

	// mock /run/mnt
	s.runMnt = c.MkDir()
	restore = main.MockRunMnt(s.runMnt)
	s.AddCleanup(restore)

	// pretend /run/mnt/ubuntu-seed has a valid seed
	s.seedDir = filepath.Join(s.runMnt, "ubuntu-seed")

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{SeedDir: s.seedDir}
	seed20.SetupAssertSigning("canonical")
	restore = seed.MockTrusted(seed20.StoreSigning.Trusted)
	s.AddCleanup(restore)

	// XXX: we don't really use this but seedtest always expects my-brand
	seed20.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	// add a bunch of snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: core20\nversion: 1\ntype: base", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)

	s.sysLabel = "20191118"
	seed20.MakeSeed(c, s.sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)
}

func (s *initramfsMountsSuite) mockProcCmdlineContent(c *C, newContent string) {
	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	err := ioutil.WriteFile(mockProcCmdline, []byte(newContent), 0644)
	c.Assert(err, IsNil)
	restore := boot.MockProcCmdline(mockProcCmdline)
	s.AddCleanup(restore)
}

func (s *initramfsMountsSuite) TestInitramfsMountsNoModeError(c *C) {
	s.mockProcCmdlineContent(c, "nothing-to-see")

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot detect mode nor recovery system to use")
}

func (s *initramfsMountsSuite) TestInitramfsMountsUnknownMode(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install-foo")

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, `cannot use unknown mode "install-foo"`)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep1(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode= snapd_recovery_system="+s.sysLabel)

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf("/dev/disk/by-label/ubuntu-seed %s/ubuntu-seed\n", s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep2(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "snapd"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/snaps/snapd_1.snap %[2]s/snapd
%[1]s/snaps/pc-kernel_1.snap %[2]s/kernel
%[1]s/snaps/core20_1.snap %[2]s/base
--type=tmpfs tmpfs /run/mnt/ubuntu-data
`, s.seedDir, s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep4(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "snapd"))
			return true, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, "")
	modeEnv := filepath.Join(s.runMnt, "/ubuntu-data/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep1(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return false, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return false, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-label/ubuntu-seed %[1]s/ubuntu-seed
/dev/disk/by-label/ubuntu-boot %[1]s/ubuntu-boot
/dev/disk/by-label/ubuntu-data %[1]s/ubuntu-data
`, s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep2(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := boot.Modeenv{
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeBaseSnapUpgradeFailsHappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv as if we failed to boot and were rebooted because the
	// base snap was broken
	modeEnv := &boot.Modeenv{
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryingStatus,
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	tryBaseSnap := filepath.Join(s.runMnt, "ubuntu-data", "system-data", dirs.SnapBlobDir, "core20_124.snap")
	err = os.MkdirAll(filepath.Dir(tryBaseSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryBaseSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)
	defer os.Remove(tryBaseSnap)

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, s.runMnt))

	// check that the modeenv was re-written
	newModeenv, err := boot.ReadModeenv(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)
	// BaseStatus was re-set to default
	c.Assert(newModeenv.BaseStatus, DeepEquals, boot.DefaultStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeModeenvTryBaseEmptyHappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write a modeenv with no try_base so we fall back to using base
	modeEnv := &boot.Modeenv{
		Base:       "core20_123.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, s.runMnt))

	// check that the modeenv is the same
	newModeenv, err := boot.ReadModeenv(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, modeEnv.BaseStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeBaseSnapUpgradeHappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := &boot.Modeenv{
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	tryBaseSnap := filepath.Join(s.runMnt, "ubuntu-data", "system-data", dirs.SnapBlobDir, "core20_124.snap")
	err = os.MkdirAll(filepath.Dir(tryBaseSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryBaseSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)
	defer os.Remove(tryBaseSnap)

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/core20_124.snap %[1]s/base
`, s.runMnt))

	// check that the modeenv was re-written
	newModeenv, err := boot.ReadModeenv(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, boot.TryingStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeModeenvBaseEmptyUnhappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write an empty modeenv
	modeEnv := &boot.Modeenv{}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "modeenv corrupt: missing base setting")
	c.Assert(n, Equals, 4)
	c.Check(s.Stdout.String(), Equals, "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeModeenvTryBaseNotExistsHappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return true, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write a modeenv with try_base not existing on disk so we fall back to
	// using the normal base
	modeEnv := &boot.Modeenv{
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, s.runMnt))

	// check that the modeenv is the same
	newModeenv, err := boot.ReadModeenv(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, modeEnv.BaseStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeKernelSnapUpgradeHappy(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return true, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := &boot.Modeenv{
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	// set the try kernel
	tryKernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r = bloader.SetRunKernelImageEnabledTryKernel(tryKernel)
	defer r()

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/pc-kernel_2.snap %[1]s/kernel
`, s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeUntrustedKernelSnap(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return true, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := boot.Modeenv{
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel as a kernel not in CurrentKernels
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`fallback kernel.efi snap %q is not trusted in the modeenv \(should be %q\)`, "pc-kernel_2.snap", "pc-kernel_1.snap"))
	c.Assert(n, Equals, 5)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeTryingKernelSnapSymlinksSwitched(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return true, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := &boot.Modeenv{
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current try-kernel.efi symlink as the expected fallback kernel snap
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledTryKernel(kernel)
	defer r()

	// set the current kernel.efi symlink as the expected try kernel snap
	tryKernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r = bloader.SetRunKernelImageEnabledKernel(tryKernel)
	defer r()

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`fallback kernel.efi snap %q is not trusted in the modeenv \(should be %q\)`, "pc-kernel_2.snap", "pc-kernel_1.snap"))
	c.Assert(n, Equals, 5)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeUntrustedTryKernelSnapFallsBack(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-boot"))
			return true, nil
		case 3:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return true, nil
		case 4:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return true, nil
		case 5:
			c.Check(path, Equals, filepath.Join(s.runMnt, "kernel"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	// write modeenv
	modeEnv := boot.Modeenv{
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.Write(filepath.Join(s.runMnt, "ubuntu-data", "system-data"))
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the try kernel as a kernel not in CurrentKernels
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledTryKernel(kernel2)
	defer r()

	// set the normal kernel as a valid kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = bloader.SetRunKernelImageEnabledKernel(kernel1)
	defer r()

	_, err = main.Parser.ParseArgs([]string{"initramfs-mounts"})

	// TODO:UC20: if we have somewhere to log errors from snap-bootstrap during
	// the initramfs, check that log here
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/ubuntu-data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, s.runMnt))
}
