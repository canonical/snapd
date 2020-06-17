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

package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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
	sysLabel string
	model    *asserts.Model
}

var _ = Suite(&initramfsMountsSuite{})

// because 1.9 vet does not like xerrors.Errorf(".. %w")
type mockedWrappedError struct {
	err error
	fmt string
}

func (m *mockedWrappedError) Unwrap() error { return m.err }

func (m *mockedWrappedError) Error() string { return fmt.Sprintf(m.fmt, m.err) }

func (s *initramfsMountsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.Stdout = bytes.NewBuffer(nil)
	restore := main.MockStdout(s.Stdout)
	s.AddCleanup(restore)

	_, restore = logger.MockLogger()
	s.AddCleanup(restore)

	// mock /run/mnt
	dirs.SetRootDir(c.MkDir())
	restore = func() { dirs.SetRootDir("") }
	s.AddCleanup(restore)

	// pretend /run/mnt/ubuntu-seed has a valid seed
	s.seedDir = boot.InitramfsUbuntuSeedDir

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
	s.model = seed20.MakeSeed(c, s.sysLabel, "my-brand", "my-model", map[string]interface{}{
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

func makeSnapFilesOnEarlyBootUbuntuData(c *C, snapFilenames ...string) {
	// also make sure the snaps also exist on ubuntu-data
	snapDir := dirs.SnapBlobDirUnder(boot.InitramfsWritableDir)
	err := os.MkdirAll(snapDir, 0755)
	c.Assert(err, IsNil)
	for _, sn := range snapFilenames {
		err = ioutil.WriteFile(filepath.Join(snapDir, sn), nil, 0644)
		c.Assert(err, IsNil)
	}
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

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "cannot detect mode nor recovery system to use")
}

func (s *initramfsMountsSuite) TestInitramfsMountsUnknownMode(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install-foo")

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, `cannot use unknown mode "install-foo"`)
}

// these types represent lists of expected mount directories to be
// checked with IsMounted with an associated mounted state to simulate
type (
	expectedMountDirs interface {
		size() int
		// dirAndIsMounted returns the dir expected for the
		// IsMounted call with relative call number callNum
		// plus the simulated mounted state
		dirAndIsMounted(callNum int) (dir string, mounted bool)
	}
	mounted       []string
	notYetMounted []string
)

func (m mounted) size() int                                            { return len(m) }
func (m mounted) dirAndIsMounted(callNum int) (dir string, state bool) { return m[callNum], true }

func (n notYetMounted) size() int { return len(n) }
func (n notYetMounted) dirAndIsMounted(callNum int) (dir string, state bool) {
	return n[callNum], false
}

func (s *initramfsMountsSuite) mockExpectedMountChecks(c *C, expectedDirs ...expectedMountDirs) *int {
	var n int // call counter
	r := main.MockOsutilIsMounted(func(path string) (bool, error) {
		callNum := n
		n++
		// find expected covering callNum
		for _, expected := range expectedDirs {
			// is callNum within expected?
			if callNum < expected.size() {
				dir, mounted := expected.dirAndIsMounted(callNum)
				c.Check(path, Equals, dir)
				return mounted, nil
			}
			// adjust callNum for indexing within the next expected
			callNum -= expected.size()
		}
		return false, fmt.Errorf("unexpected number of calls: %v", n)
	})
	s.AddCleanup(r)
	return &n
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep1(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuSeedDir},
	)

	// mock that we don't know which partition uuid the kernel was booted from
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf("/dev/disk/by-label/ubuntu-seed %s/ubuntu-seed\n", boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep1BootedKernelPartitionUUID(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuSeedDir},
	)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf("/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid %s/ubuntu-seed\n", boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep2(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		mounted{boot.InitramfsUbuntuSeedDir},
		notYetMounted{
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			boot.InitramfsDataDir,
		},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/snaps/snapd_1.snap %[2]s/snapd
%[1]s/snaps/pc-kernel_1.snap %[2]s/kernel
%[1]s/snaps/core20_1.snap %[2]s/base
--type=tmpfs tmpfs %[2]s/data
`, s.seedDir, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep3(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		mounted{boot.InitramfsUbuntuSeedDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			boot.InitramfsDataDir,
		},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, "")
	modeEnv := dirs.SnapModeenvFileUnder(boot.InitramfsWritableDir)
	c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
`)
	cloudInitDisable := filepath.Join(boot.InitramfsWritableDir, "_writable_defaults/etc/cloud/cloud-init.disabled")
	c.Check(cloudInitDisable, testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep1(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuBootDir},
	)

	// mock that we don't know which partition uuid the kernel was booted from
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-label/ubuntu-boot %[1]s/ubuntu-boot
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep1WithBootedKernelPartUUID(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuBootDir},
	)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-boot-partuuid")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-partuuid/specific-ubuntu-boot-partuuid %[1]s/ubuntu-boot
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep2(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{boot.InitramfsUbuntuBootDir},
		notYetMounted{boot.InitramfsUbuntuSeedDir},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 2)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-label/ubuntu-seed %[1]s/ubuntu-seed
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep3(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
		},
		notYetMounted{boot.InitramfsDataDir},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 3)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-label/ubuntu-data %[1]s/data
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep3EncryptedData(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	// write the installed model like makebootable does it
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)
	mf, err := os.Create(filepath.Join(boot.InitramfsUbuntuBootDir, "model"))
	c.Assert(err, IsNil)
	defer mf.Close()
	err = asserts.NewEncoder(mf).Encode(s.model)
	c.Assert(err, IsNil)

	activated := false
	restore := main.MockSecbootUnlockVolumeIfEncrypted(func(name string, lockKeysOnFinish bool) (string, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(lockKeysOnFinish, Equals, true)
		activated = true
		return "path-to-device", nil
	})
	defer restore()

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
		},
		notYetMounted{boot.InitramfsDataDir},
	)

	measureEpochCalls := 0
	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	var measuredModel *asserts.Model
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		var err error
		measuredModel, err = findModel()
		if err != nil {
			return err
		}
		return nil
	})
	defer restore()

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(*n, Equals, 3)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`path-to-device %s/data
`, boot.InitramfsRunMntDir))
	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "run-model-measured"), testutil.FilePresent)
}

func (s *initramfsMountsSuite) TestInitramfsMountsStep3EncryptedNoModelRun(c *C) {
	s.testInitramfsMountsStep3EncryptedNoModel(c, "run", "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsStep3EncryptedNoModelInstall(c *C) {
	s.testInitramfsMountsStep3EncryptedNoModel(c, "install", s.sysLabel)
}

func (s *initramfsMountsSuite) TestInitramfsMountsStep1EncryptedNoModelRecovery(c *C) {
	s.testInitramfsMountsStep3EncryptedNoModel(c, "recover", s.sysLabel)
}

func (s *initramfsMountsSuite) testInitramfsMountsStep3EncryptedNoModel(c *C, mode, label string) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s", mode))
	if label != "" {
		s.mockProcCmdlineContent(c,
			fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, label))
		// break the seed
		err := os.Remove(filepath.Join(s.seedDir, "systems", label, "model"))
		c.Assert(err, IsNil)
	}

	restore := main.MockOsutilIsMounted(func(path string) (bool, error) {
		return true, nil
	})
	defer restore()

	measureEpochCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		_, err := findModel()
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected call")
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	where := "/run/mnt/ubuntu-boot/model"
	if mode != "run" {
		where = fmt.Sprintf("/run/mnt/ubuntu-seed/systems/%s/model", label)
	}
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot read model assertion: open .*%s: no such file or directory", where))
	c.Assert(measureEpochCalls, Equals, 1)
	c.Assert(measureModelCalls, Equals, 1)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	gl, err := filepath.Glob(filepath.Join(dirs.SnapBootstrapRunDir, "*-model-measured"))
	c.Assert(err, IsNil)
	c.Assert(gl, HasLen, 0)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
		},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191118",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	defer r()

	makeSnapFilesOnEarlyBootUbuntuData(c, kernel.Filename(), "core20_123.snap")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 6)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
%[1]s/ubuntu-seed/snaps/snapd_1.snap %[1]s/snapd
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4BaseSnapUpgradeFailsHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "base")},
		mounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv as if we failed to boot and were rebooted because the
	// base snap was broken
	modeEnv := &boot.Modeenv{
		Mode:       "run",
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryingStatus,
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	makeSnapFilesOnEarlyBootUbuntuData(c, "core20_123.snap", "core20_124.snap")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, boot.InitramfsRunMntDir))

	// check that the modeenv was re-written
	newModeenv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)
	// BaseStatus was re-set to default
	c.Assert(newModeenv.BaseStatus, DeepEquals, boot.DefaultStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4ModeenvTryBaseEmptyHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "base")},
		mounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write a modeenv with no try_base so we fall back to using base
	modeEnv := &boot.Modeenv{
		Mode:       "run",
		Base:       "core20_123.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	makeSnapFilesOnEarlyBootUbuntuData(c, "core20_123.snap")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, boot.InitramfsRunMntDir))

	// check that the modeenv is the same
	newModeenv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, modeEnv.BaseStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4BaseSnapUpgradeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "base")},
		mounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := &boot.Modeenv{
		Mode:       "run",
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	makeSnapFilesOnEarlyBootUbuntuData(c, "core20_123.snap", "core20_124.snap")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_124.snap %[1]s/base
`, boot.InitramfsRunMntDir))

	// check that the modeenv was re-written
	newModeenv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, boot.TryingStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4ModeenvBaseEmptyUnhappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "base")},
		mounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write an empty modeenv
	modeEnv := &boot.Modeenv{
		Mode: "run",
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, "fallback base snap unusable: cannot get snap revision: modeenv base boot variable is empty")
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4ModeenvTryBaseNotExistsHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "base")},
		mounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write a modeenv with try_base not existing on disk so we fall back to
	// using the normal base
	modeEnv := &boot.Modeenv{
		Mode:       "run",
		Base:       "core20_123.snap",
		TryBase:    "core20_124.snap",
		BaseStatus: boot.TryStatus,
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	makeSnapFilesOnEarlyBootUbuntuData(c, "core20_123.snap")

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
`, boot.InitramfsRunMntDir))

	// check that the modeenv is the same
	newModeenv, err := boot.ReadModeenv(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)
	c.Assert(newModeenv.BaseStatus, DeepEquals, modeEnv.BaseStatus)
	c.Assert(newModeenv.TryBase, DeepEquals, modeEnv.TryBase)
	c.Assert(newModeenv.Base, DeepEquals, modeEnv.Base)
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4KernelSnapUpgradeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := &boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	defer r()

	// set the try kernel
	tryKernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r = bloader.SetEnabledTryKernel(tryKernel)
	defer r()

	makeSnapFilesOnEarlyBootUbuntuData(c, kernel.Filename(), tryKernel.Filename())

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_2.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

// TODO:UC20: in this case snap-bootstrap should request a reboot, since we
//            already booted the try snap, so mounting the fallback kernel will
//            not match in some cases
func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4UntrustedKernelSnap(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel as a kernel not in CurrentKernels
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	defer r()

	makeSnapFilesOnEarlyBootUbuntuData(c, kernel.Filename())

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, fmt.Sprintf("fallback kernel snap %q is not trusted in the modeenv", "pc-kernel_2.snap"))
	c.Assert(*n, Equals, 5)
}

// TODO:UC20: in this case snap-bootstrap should request a reboot, since we
//            already booted the try snap, so mounting the fallback kernel will
//            not match in some cases
func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4UntrustedTryKernelSnapFallsBack(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the try kernel as a kernel not in CurrentKernels
	kernel2, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledTryKernel(kernel2)
	defer r()

	// set the normal kernel as a valid kernel
	kernel1, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = bloader.SetEnabledKernel(kernel1)
	defer r()

	makeSnapFilesOnEarlyBootUbuntuData(c, kernel1.Filename(), kernel2.Filename())

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})

	// TODO:UC20: if we have somewhere to log errors from snap-bootstrap during
	// the initramfs, check that log here
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4KernelStatusTryingNoTryKernel(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// we are in trying mode, but don't set a try-kernel so we fallback to the
	// fallback kernel
	err = bloader.SetBootVars(map[string]string{"kernel_status": boot.TryingStatus})
	c.Assert(err, IsNil)

	// set the normal kernel as a valid kernel
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	defer r()

	makeSnapFilesOnEarlyBootUbuntuData(c, kernel.Filename())

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})

	// TODO:UC20: if we have somewhere to log errors from snap-bootstrap during
	// the initramfs, check that log here
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4EnvRefKernelBootstate(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
		},
		notYetMounted{
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
		},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191118",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	bloader.SetBootKernel("pc-kernel_1.snap")

	// ensure the kernel snap and base snap files exist though
	kernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_1.snap")
	err = os.MkdirAll(filepath.Dir(kernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(kernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	baseSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "core20_123.snap")
	err = os.MkdirAll(filepath.Dir(baseSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(baseSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 6)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/core20_123.snap %[1]s/base
%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
%[1]s/ubuntu-seed/snaps/snapd_1.snap %[1]s/snapd
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4EnvRefKernelBootstateKernelSnapUpgradeHappy(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := &boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap", "pc-kernel_2.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	tryBaseSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "core20_124.snap")
	err = os.MkdirAll(filepath.Dir(tryBaseSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryBaseSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)
	defer os.Remove(tryBaseSnap)

	// mock a bootloader
	bloader := boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	bloader.BootVars["kernel_status"] = boot.TryingStatus

	// set the current kernel and try kernel
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootTryKernel("pc-kernel_2.snap")

	// ensure the kernel snap and base snap files exist though
	kernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_1.snap")
	err = os.MkdirAll(filepath.Dir(kernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(kernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	tryKernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_2.snap")
	err = os.MkdirAll(filepath.Dir(tryKernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryKernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_2.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

// TODO:UC20: in this case snap-bootstrap should request a reboot, since we
//            already booted the try snap, so mounting the fallback kernel will
//            not match in some cases
func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4EnvRefKernelBootstateUntrustedKernelSnap(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel as a kernel not in CurrentKernels
	bloader.SetBootKernel("pc-kernel_2.snap")

	// ensure the new kernel snap file exists though
	tryKernelSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_2.snap")
	err = os.MkdirAll(filepath.Dir(tryKernelSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryKernelSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, ErrorMatches, fmt.Sprintf("fallback kernel snap %q is not trusted in the modeenv", "pc-kernel_2.snap"))
	c.Assert(*n, Equals, 5)
}

// TODO:UC20: in this case snap-bootstrap should request a reboot, since we
//            already booted the try snap, so mounting the fallback kernel will
//            not match in some cases
func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4EnvRefKernelBootstateUntrustedTryKernelSnapFallsBack(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the try kernel as a kernel not in CurrentKernels
	bloader.SetBootTryKernel("pc-kernel_2.snap")

	// set the normal kernel as a valid kernel
	bloader.SetBootKernel("pc-kernel_1.snap")

	bloader.BootVars["kernel_status"] = boot.TryingStatus

	// ensure the kernel snap and base snap files exist though
	kernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_1.snap")
	err = os.MkdirAll(filepath.Dir(kernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(kernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	tryKernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_2.snap")
	err = os.MkdirAll(filepath.Dir(tryKernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(tryKernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})

	// TODO:UC20: if we have somewhere to log errors from snap-bootstrap during
	// the initramfs, check that log here
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRunModeStep4EnvRefKernelBootstateKernelStatusTryingNoTryKernel(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=run")

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuBootDir,
			boot.InitramfsUbuntuSeedDir,
			boot.InitramfsDataDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "kernel")},
	)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           "core20_123.snap",
		CurrentKernels: []string{"pc-kernel_1.snap"},
	}
	err := modeEnv.WriteTo(boot.InitramfsWritableDir)
	c.Assert(err, IsNil)

	// mock a bootloader
	bloader := boottest.MockUC20EnvRefExtractedKernelRunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// we are in trying mode, but don't set a try-kernel so we fallback to the
	// fallback kernel
	err = bloader.SetBootVars(map[string]string{"kernel_status": boot.TryingStatus})
	c.Assert(err, IsNil)

	// set the normal kernel as a valid kernel
	bloader.SetBootKernel("pc-kernel_1.snap")

	kernSnap := filepath.Join(dirs.SnapBlobDirUnder(boot.InitramfsWritableDir), "pc-kernel_1.snap")
	err = os.MkdirAll(filepath.Dir(kernSnap), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(kernSnap, []byte{0}, 0644)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})

	// TODO:UC20: if we have somewhere to log errors from snap-bootstrap during
	// the initramfs, check that log here
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/data/system-data/var/lib/snapd/snaps/pc-kernel_1.snap %[1]s/kernel
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep1(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuSeedDir},
	)

	// mock that we don't know which partition uuid the kernel was booted from
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf("/dev/disk/by-label/ubuntu-seed %s/ubuntu-seed\n", boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep1BootedKernelPartitionUUID(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		notYetMounted{boot.InitramfsUbuntuSeedDir},
	)

	restore := main.MockPartitionUUIDForBootedKernelDisk("specific-ubuntu-seed-partuuid")
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 1)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf("/dev/disk/by-partuuid/specific-ubuntu-seed-partuuid %s/ubuntu-seed\n", boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep2(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		mounted{boot.InitramfsUbuntuSeedDir},
		notYetMounted{
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			filepath.Join(boot.InitramfsRunMntDir, "data"),
		},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 5)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/snaps/snapd_1.snap %[2]s/snapd
%[1]s/snaps/pc-kernel_1.snap %[2]s/kernel
%[1]s/snaps/core20_1.snap %[2]s/base
--type=tmpfs tmpfs %[2]s/data
`, s.seedDir, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep3(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuSeedDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			filepath.Join(boot.InitramfsRunMntDir, "data"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data")},
	)

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 6)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`/dev/disk/by-label/ubuntu-data %s/host/ubuntu-data
`, boot.InitramfsRunMntDir))
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep3Encrypted(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	activated := false
	restore := main.MockSecbootUnlockVolumeIfEncrypted(func(name string, lockKeysOnFinish bool) (string, error) {
		c.Assert(name, Equals, "ubuntu-data")
		c.Assert(lockKeysOnFinish, Equals, true)
		activated = true
		return "path-to-device", nil
	})
	defer restore()

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuSeedDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			filepath.Join(boot.InitramfsRunMntDir, "data"),
		},
		notYetMounted{filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data")},
	)

	measureEpochCalls := 0
	measureModelCalls := 0
	restore = main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	var measuredModel *asserts.Model
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		var err error
		measuredModel, err = findModel()
		if err != nil {
			return err
		}
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 6)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`path-to-device %s/host/ubuntu-data
`, boot.InitramfsRunMntDir))

	c.Check(activated, Equals, true)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Check(measuredModel, DeepEquals, s.model)

	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, fmt.Sprintf("%s-model-measured", s.sysLabel)), testutil.FilePresent)
}

var mockStateContent = `{"data":{"auth":{"users":[{"id":1,"name":"mvo"}],"macaroon-key":"not-a-cookie","last-id":1}},"some":{"other":"stuff"}}`

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep4(c *C) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover snapd_recovery_system="+s.sysLabel)

	n := s.mockExpectedMountChecks(c,
		mounted{
			boot.InitramfsUbuntuSeedDir,
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			filepath.Join(boot.InitramfsRunMntDir, "data"),
			filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data"),
		},
	)

	ephemeralUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "data/")
	err := os.MkdirAll(ephemeralUbuntuData, 0755)
	c.Assert(err, IsNil)
	// mock a auth data in the host's ubuntu-data
	hostUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data/")
	err = os.MkdirAll(hostUbuntuData, 0755)
	c.Assert(err, IsNil)
	mockCopiedFiles := []string{
		// extrausers
		"system-data/var/lib/extrausers/passwd",
		"system-data/var/lib/extrausers/shadow",
		"system-data/var/lib/extrausers/group",
		"system-data/var/lib/extrausers/gshadow",
		// sshd
		"system-data/etc/ssh/ssh_host_rsa.key",
		"system-data/etc/ssh/ssh_host_rsa.key.pub",
		// user ssh
		"user-data/user1/.ssh/authorized_keys",
		"user-data/user2/.ssh/authorized_keys",
		// user snap authentication
		"user-data/user1/.snap/auth.json",
		// sudoers
		"system-data/etc/sudoers.d/create-user-test",
		// netplan networking
		"system-data/etc/netplan/00-snapd-config.yaml", // example console-conf filename
		"system-data/etc/netplan/50-cloud-init.yaml",   // example cloud-init filename
		// systemd clock file
		"system-data/var/lib/systemd/timesync/clock",
	}
	mockUnrelatedFiles := []string{
		"system-data/var/lib/foo",
		"system-data/etc/passwd",
		"user-data/user1/some-random-data",
		"user-data/user2/other-random-data",
		"user-data/user2/.snap/sneaky-not-auth.json",
		"system-data/etc/not-networking/netplan",
		"system-data/var/lib/systemd/timesync/clock-not-the-clock",
	}
	for _, mockFile := range append(mockCopiedFiles, mockUnrelatedFiles...) {
		p := filepath.Join(hostUbuntuData, mockFile)
		err = os.MkdirAll(filepath.Dir(p), 0750)
		c.Assert(err, IsNil)
		mockContent := fmt.Sprintf("content of %s", filepath.Base(mockFile))
		err = ioutil.WriteFile(p, []byte(mockContent), 0640)
		c.Assert(err, IsNil)
	}
	// create a mock state
	mockedState := filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	err = os.MkdirAll(filepath.Dir(mockedState), 0750)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockedState, []byte(mockStateContent), 0640)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Assert(*n, Equals, 6)
	c.Check(s.Stdout.String(), Equals, "")

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
`)
	for _, p := range mockUnrelatedFiles {
		c.Check(filepath.Join(ephemeralUbuntuData, p), testutil.FileAbsent)
	}
	for _, p := range mockCopiedFiles {
		c.Check(filepath.Join(ephemeralUbuntuData, p), testutil.FilePresent)
		fi, err := os.Stat(filepath.Join(ephemeralUbuntuData, p))
		// check file mode is set
		c.Assert(err, IsNil)
		c.Check(fi.Mode(), Equals, os.FileMode(0640))
		// check dir mode is set in parent dir
		fiParent, err := os.Stat(filepath.Dir(filepath.Join(ephemeralUbuntuData, p)))
		c.Assert(err, IsNil)
		c.Check(fiParent.Mode(), Equals, os.FileMode(os.ModeDir|0750))
	}

	c.Check(filepath.Join(ephemeralUbuntuData, "system-data/var/lib/snapd/state.json"), testutil.FileEquals, `{"data":{"auth":{"last-id":1,"macaroon-key":"not-a-cookie","users":[{"id":1,"name":"mvo"}]}},"changes":{},"tasks":{},"last-change-id":0,"last-task-id":0,"last-lane-id":0}`)
}

func (s *initramfsMountsSuite) testInitramfsMountsInstallRecoverModeStep2Measure(c *C, mode string) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, s.sysLabel))

	n := s.mockExpectedMountChecks(c,
		mounted{boot.InitramfsUbuntuSeedDir},
		notYetMounted{
			filepath.Join(boot.InitramfsRunMntDir, "base"),
			filepath.Join(boot.InitramfsRunMntDir, "kernel"),
			filepath.Join(boot.InitramfsRunMntDir, "snapd"),
			filepath.Join(boot.InitramfsRunMntDir, "data"),
		},
	)

	measureEpochCalls := 0
	measureModelCalls := 0
	restore := main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		measureEpochCalls++
		return nil
	})
	defer restore()

	var measuredModel *asserts.Model
	restore = main.MockSecbootMeasureSnapModelWhenPossible(func(findModel func() (*asserts.Model, error)) error {
		measureModelCalls++
		var err error
		measuredModel, err = findModel()
		if err != nil {
			return err
		}
		return nil
	})
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout.String(), Equals, fmt.Sprintf(`%[1]s/snaps/snapd_1.snap %[2]s/snapd
%[1]s/snaps/pc-kernel_1.snap %[2]s/kernel
%[1]s/snaps/core20_1.snap %[2]s/base
--type=tmpfs tmpfs %[2]s/data
`, s.seedDir, boot.InitramfsRunMntDir))
	c.Check(measuredModel, NotNil)
	c.Check(measuredModel, DeepEquals, s.model)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, s.sysLabel+"-model-measured"), testutil.FilePresent)
	c.Check(*n, Equals, 5)
}

func (s *initramfsMountsSuite) TestInitramfsMountsInstallModeStep2Measure(c *C) {
	s.testInitramfsMountsInstallRecoverModeStep2Measure(c, "")
}

func (s *initramfsMountsSuite) TestInitramfsMountsRecoverModeStep2Measure(c *C) {
	s.testInitramfsMountsInstallRecoverModeStep2Measure(c, "recover")
}
