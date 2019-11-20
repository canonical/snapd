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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts/assertstest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
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

	seedDir string
	runMnt  string
}

var _ = Suite(&initramfsMountsSuite{})

func (s *initramfsMountsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	restore := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.BaseTest.AddCleanup(restore)

	s.Stdout = bytes.NewBuffer(nil)
	restore = main.MockStdout(s.Stdout)
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

	sysLabel := "20191118"
	seed20.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
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
	restore := main.MockProcCmdline(mockProcCmdline)
	s.AddCleanup(restore)
}

func (s *initramfsMountsSuite) TestInitramfsMountsNoModeError(c *C) {
	s.mockProcCmdlineContent(c, "nothing-to-see")

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot detect if in run,install,recover mode")
}

func (s *initramfsMountsSuite) TestInitramfsMountsNoRunModeYet(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "run mode mount generation not implemented yet")
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep1(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=")

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
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=")

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		n++
		switch n {
		case 1:
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-seed"))
			return true, nil
		case 2:
			c.Check(path, Equals, filepath.Join(s.runMnt, "base"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 2)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/snaps/pc-kernel_1.snap %[2]s/kernel
%[1]s/snaps/core20_1.snap %[2]s/base
`, s.seedDir, s.runMnt))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep3(c *C) {
	n := 0
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install")

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
			c.Check(path, Equals, filepath.Join(s.runMnt, "ubuntu-data"))
			return false, nil
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	defer restore()

	_, err := main.Parser.ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 3)
	c.Check(s.Stdout.String(), Equals, "--type=tmpfs tmpfs /run/mnt/ubuntu-data\n")
}
