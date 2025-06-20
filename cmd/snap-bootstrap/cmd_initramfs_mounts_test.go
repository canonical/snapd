// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C)20 19-2024 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

var brandPrivKey, _ = assertstest.GenerateKey(752)

var (
	tmpfsMountOpts = &main.SystemdMountOptions{
		Tmpfs:   true,
		NoSuid:  true,
		Private: true,
	}
	needsNoSuidNoDevNoExecMountOpts = &main.SystemdMountOptions{
		Private: true,
		NoSuid:  true,
		NoExec:  true,
		NoDev:   true,
	}
	needsFsckAndNoSuidNoDevNoExecMountOpts = &main.SystemdMountOptions{
		Private:   true,
		NeedsFsck: true,
		NoSuid:    true,
		NoExec:    true,
		NoDev:     true,
	}
	needsFsckNoPrivateDiskMountOpts = &main.SystemdMountOptions{
		NeedsFsck: true,
	}
	needsFsckDiskMountOpts = &main.SystemdMountOptions{
		NeedsFsck: true,
		Private:   true,
	}
	needsFsckAndNoSuidDiskMountOpts = &main.SystemdMountOptions{
		NeedsFsck: true,
		NoSuid:    true,
		Private:   true,
	}
	needsNoSuidDiskMountOpts = &main.SystemdMountOptions{
		NoSuid:  true,
		Private: true,
	}
	snapMountOpts = &main.SystemdMountOptions{
		ReadOnly: true,
		Private:  true,
	}
	bindOpts = &main.SystemdMountOptions{
		Bind: true,
	}

	seedPart = disks.Partition{
		FilesystemLabel:  "ubuntu-seed",
		PartitionUUID:    "ubuntu-seed-partuuid",
		KernelDeviceNode: "/dev/sda2",
	}

	seedPartCapitalFsLabel = disks.Partition{
		FilesystemLabel:  "UBUNTU-SEED",
		FilesystemType:   "vfat",
		PartitionUUID:    "ubuntu-seed-partuuid",
		KernelDeviceNode: "/dev/sda2",
	}

	bootPart = disks.Partition{
		FilesystemLabel:  "ubuntu-boot",
		PartitionUUID:    "ubuntu-boot-partuuid",
		KernelDeviceNode: "/dev/sda3",
	}

	savePart = disks.Partition{
		FilesystemLabel:  "ubuntu-save",
		PartitionUUID:    "ubuntu-save-partuuid",
		KernelDeviceNode: "/dev/sda4",
	}

	dataPart = disks.Partition{
		FilesystemLabel:  "ubuntu-data",
		PartitionUUID:    "ubuntu-data-partuuid",
		KernelDeviceNode: "/dev/sda5",
	}

	saveEncPart = disks.Partition{
		FilesystemLabel:  "ubuntu-save-enc",
		PartitionUUID:    "ubuntu-save-enc-partuuid",
		KernelDeviceNode: "/dev/sda4",
	}

	dataEncPart = disks.Partition{
		FilesystemLabel:  "ubuntu-data-enc",
		PartitionUUID:    "ubuntu-data-enc-partuuid",
		KernelDeviceNode: "/dev/sda5",
	}

	// a boot disk without ubuntu-save
	defaultBootDisk = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			dataPart,
		},
		DiskHasPartitions: true,
		DevNum:            "default",
	}

	defaultBootWithSaveDisk = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPart,
			bootPart,
			dataPart,
			savePart,
		},
		DiskHasPartitions: true,
		DevNum:            "default-with-save",
	}

	defaultBootWithSeedPartCapitalFsLabel = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			seedPartCapitalFsLabel,
			bootPart,
			dataPart,
			savePart,
		},
		DiskHasPartitions: true,
		DevNum:            "default-with-save",
	}

	defaultEncBootDisk = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			bootPart,
			seedPart,
			dataEncPart,
			saveEncPart,
		},
		DiskHasPartitions: true,
		DevNum:            "defaultEncDev",
	}

	// a boot disk without ubuntu-seed, which can happen for classic
	defaultNoSeedWithSaveDisk = &disks.MockDiskMapping{
		Structure: []disks.Partition{
			bootPart,
			dataPart,
			savePart,
		},
		DiskHasPartitions: true,
		DevNum:            "default-no-seed-with-save",
	}

	mockStateContent = `{"data":{"auth":{"users":[{"id":1,"name":"mvo"}],"macaroon-key":"not-a-cookie","last-id":1}},"some":{"other":"stuff"}}`
)

// helpers to create consistent UnlockResult values

func foundUnencrypted(name string) secboot.UnlockResult {
	dev := filepath.Join("/dev/disk/by-partuuid", name+"-partuuid")
	return secboot.UnlockResult{
		PartDevice: dev,
		FsDevice:   dev,
	}
}

func happyUnlocked(name string, method secboot.UnlockMethod) secboot.UnlockResult {
	return secboot.UnlockResult{
		PartDevice:   filepath.Join("/dev/disk/by-partuuid", name+"-enc-partuuid"),
		FsDevice:     filepath.Join("/dev/mapper", name+"-random"),
		IsEncrypted:  true,
		UnlockMethod: method,
	}
}

func foundEncrypted(name string) secboot.UnlockResult {
	return secboot.UnlockResult{
		PartDevice: filepath.Join("/dev/disk/by-partuuid", name+"-enc-partuuid"),
		// FsDevice is empty if we didn't unlock anything
		FsDevice:    "",
		IsEncrypted: true,
	}
}

func notFoundPart() secboot.UnlockResult {
	return secboot.UnlockResult{}
}

// other helpers that are common between modes

func writeGadget(c *C, espName, espRole, espLabel string) {
	gadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ` + espName

	if espRole != "" {
		gadgetYaml += `
        role: ` + espRole
	}
	if espLabel != "" {
		gadgetYaml += `
        filesystem-label: ` + espLabel
	}

	gadgetYaml += `
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 99M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        offset: 1202M
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 4312776192
`
	var err error
	gadgetDir := filepath.Join(boot.InitramfsRunMntDir, "gadget", "meta")
	err = os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = osutil.AtomicWriteFile(filepath.Join(gadgetDir, "gadget.yaml"), []byte(gadgetYaml), 0644, 0)
	c.Assert(err, IsNil)
}

func checkDegradedJSON(c *C, name string, exp map[string]any) {
	b, err := os.ReadFile(filepath.Join(dirs.SnapBootstrapRunDir, name))
	c.Assert(err, IsNil)
	degradedJSONObj := make(map[string]any)
	err = json.Unmarshal(b, &degradedJSONObj)
	c.Assert(err, IsNil)

	c.Assert(degradedJSONObj, DeepEquals, exp)
}

func checkKernelMounts(c *C, dataRootfs, snapRoot string, compsExist, compsExistRevs, compsNotExist, compsNotExistRevs []string) {
	// Check mount units for the drivers tree
	unitsPath := filepath.Join(dirs.GlobalRootDir, "run/systemd/system")
	snapMntDir := dirs.StripRootDir(dirs.SnapMountDir)

	// kernel snap
	what := filepath.Join(dataRootfs, "var/lib/snapd/snaps/pc-kernel_1.snap")
	where := filepath.Join(snapRoot, snapMntDir, "pc-kernel/1")
	unit := systemd.EscapeUnitNamePath(where) + ".mount"
	c.Check(filepath.Join(unitsPath, unit), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount for kernel snap
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=%s
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
`, what, where))

	// kernel-modules components
	for i, comp := range compsExist {
		compName := comp
		compRev := compsExistRevs[i]
		what := filepath.Join(dataRootfs,
			"var/lib/snapd/snaps/pc-kernel+"+compName+"_"+compRev+".comp")
		where := filepath.Join(snapRoot, snapMntDir, "pc-kernel/components/mnt",
			compName, compRev)
		unit := systemd.EscapeUnitNamePath(where) + ".mount"
		c.Check(filepath.Join(unitsPath, unit), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount for kernel snap
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=%s
Type=squashfs
Options=nodev,ro,x-gdu.hide,x-gvfs-hide
`, what, where))
	}

	for i, comp := range compsNotExist {
		compName := comp
		compRev := compsNotExistRevs[i]
		where := filepath.Join(snapRoot, snapMntDir, "pc-kernel/components/mnt",
			compName, compRev)
		unit := systemd.EscapeUnitNamePath(where) + ".mount"
		c.Check(filepath.Join(unitsPath, unit), testutil.FileAbsent)
	}

	// for /lib/{modules,firmware}
	for _, subdir := range []string{"modules", "firmware"} {
		what := filepath.Join(dataRootfs, "var/lib/snapd/kernel/pc-kernel/1/lib", subdir)
		where := filepath.Join("/sysroot/usr/lib", subdir)
		unit := systemd.EscapeUnitNamePath(where) + ".mount"
		c.Check(filepath.Join(unitsPath, unit), testutil.FileEquals, fmt.Sprintf(`[Unit]
Description=Mount of kernel drivers tree
DefaultDependencies=no
After=initrd-parse-etc.service
Before=initrd-fs.target
Before=umount.target
Conflicts=umount.target

[Mount]
What=%s
Where=%s
Options=bind,shared
`, what, where))

		symlinkPath := filepath.Join(unitsPath, "initrd-fs.target.wants", unit)
		target, err := os.Readlink(symlinkPath)
		c.Assert(err, IsNil)
		c.Assert(target, Equals, "../"+unit)
	}
}

type baseInitramfsMountsSuite struct {
	testutil.BaseTest

	isClassic bool

	// makes available a bunch of helper (like MakeAssertedSnap)
	*seedtest.TestingSeed20

	Stdout *bytes.Buffer
	logs   *bytes.Buffer

	seedDir    string
	byLabelDir string
	sysLabel   string
	model      *asserts.Model
	tmpDir     string

	snapDeclAssertsTime time.Time

	kernel   snap.PlaceInfo
	kernelr2 snap.PlaceInfo
	core20   snap.PlaceInfo
	core20r2 snap.PlaceInfo
	gadget   snap.PlaceInfo
	snapd    snap.PlaceInfo
}

func (s *baseInitramfsMountsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(osutil.MockMountInfo(""))

	s.Stdout = bytes.NewBuffer(nil)

	buf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logs = buf

	s.tmpDir = c.MkDir()
	dirstest.MustMockCanonicalSnapMountDir(s.tmpDir)

	restore = main.MockOsGetenv(func(envVar string) string { return "" })
	s.AddCleanup(restore)

	// mock /run/mnt
	dirs.SetRootDir(s.tmpDir)
	restore = func() { dirs.SetRootDir("") }
	s.AddCleanup(restore)

	restore = main.MockWaitFile(func(string, time.Duration, int) error {
		return nil
	})
	s.AddCleanup(restore)

	s.AddCleanup(systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		return []byte(``), nil
	}))

	// use a specific time for all the assertions, in the future so that we can
	// set the timestamp of the model assertion to something newer than now, but
	// still older than the snap declarations by default
	s.snapDeclAssertsTime = time.Now().Add(60 * time.Minute)

	// setup the seed
	s.setupSeed(c, time.Time{}, nil, setupSeedOpts{})

	// Make sure we have a model assertion in the ubuntu-boot partition
	var err error
	err = os.MkdirAll(filepath.Join(boot.InitramfsUbuntuBootDir, "device"), 0755)
	c.Assert(err, IsNil)
	mf, err := os.Create(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	c.Assert(err, IsNil)
	defer mf.Close()
	err = asserts.NewEncoder(mf).Encode(s.model)
	c.Assert(err, IsNil)

	s.byLabelDir = filepath.Join(s.tmpDir, "dev/disk/by-label")
	err = os.MkdirAll(s.byLabelDir, 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.tmpDir, "dev/sda1"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.Symlink("../../sda1", filepath.Join(s.byLabelDir, "ubuntu-seed"))
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.tmpDir, "dev/sda2"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.Symlink("../../sda2", filepath.Join(s.byLabelDir, "ubuntu-boot"))
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.byLabelDir, "ubuntu-boot"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.tmpDir, "dev/sda"), nil, 0644)
	c.Assert(err, IsNil)

	// make test snap PlaceInfo's for various boot functionality
	s.kernel, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)

	s.core20, err = snap.ParsePlaceInfoFromSnapFileName("core20_1.snap")
	c.Assert(err, IsNil)

	s.kernelr2, err = snap.ParsePlaceInfoFromSnapFileName("pc-kernel_2.snap")
	c.Assert(err, IsNil)

	s.core20r2, err = snap.ParsePlaceInfoFromSnapFileName("core20_2.snap")
	c.Assert(err, IsNil)

	s.gadget, err = snap.ParsePlaceInfoFromSnapFileName("pc_1.snap")
	c.Assert(err, IsNil)

	s.snapd, err = snap.ParsePlaceInfoFromSnapFileName("snapd_1.snap")
	c.Assert(err, IsNil)

	// by default mock that we don't have UEFI vars, etc. to get the booted
	// kernel partition partition uuid
	s.AddCleanup(main.MockPartitionUUIDForBootedKernelDisk(""))
	s.AddCleanup(main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
		return nil
	}))
	s.AddCleanup(main.MockSecbootMeasureSnapModelWhenPossible(func(f func() (*asserts.Model, error)) error {
		c.Check(f, NotNil)
		return nil
	}))
	s.AddCleanup(main.MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		return foundUnencrypted(name), nil
	}))
	s.AddCleanup(main.MockSecbootLockSealedKeys(func() error {
		return nil
	}))

	s.AddCleanup(main.MockOsutilSetTime(func(time.Time) error {
		return nil
	}))

	s.AddCleanup(main.MockPollWaitForLabel(0))
	s.AddCleanup(main.MockPollWaitForLabelIters(1))
}

type setupSeedOpts struct {
	hasKModsComps bool
}

func (s *baseInitramfsMountsSuite) setupSeed(c *C, modelAssertTime time.Time, gadgetSnapFiles [][]string, opts setupSeedOpts) {
	base := "core20"
	channel := "20"
	if opts.hasKModsComps {
		base = "core24"
		channel = "24"
	}

	// pretend /run/mnt/ubuntu-seed has a valid seed
	s.seedDir = boot.InitramfsUbuntuSeedDir

	// now create a minimal uc20+ seed dir with snaps/assertions
	testSeed := &seedtest.TestingSeed20{SeedDir: s.seedDir}
	testSeed.SetupAssertSigning("canonical")
	restore := seed.MockTrusted(testSeed.StoreSigning.Trusted)
	s.AddCleanup(restore)

	// XXX: we don't really use this but seedtest always expects my-brand
	testSeed.Brands.Register("my-brand", brandPrivKey, map[string]any{
		"verification": "verified",
	})

	// make sure all the assertions use the same time
	testSeed.SetSnapAssertionNow(s.snapDeclAssertsTime)

	// add a bunch of snaps
	testSeed.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "canonical", testSeed.StoreSigning.Database)
	testSeed.MakeAssertedSnap(c, fmt.Sprintf("name: pc\nversion: 1\ntype: gadget\nbase: %s", base),
		gadgetSnapFiles, snap.R(1), "canonical", testSeed.StoreSigning.Database)
	testSeed.MakeAssertedSnap(c, fmt.Sprintf("name: %s\nversion: 1\ntype: base", base),
		nil, snap.R(1), "canonical", testSeed.StoreSigning.Database)

	if opts.hasKModsComps {
		testSeed.MakeAssertedSnapWithComps(c, seedtest.SampleSnapYaml["pc-kernel=24+kmods"],
			nil, snap.R(1), nil, "canonical", testSeed.StoreSigning.Database)
	} else {
		testSeed.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil,
			snap.R(1), "canonical", testSeed.StoreSigning.Database)
	}

	// pretend that by default, the model uses an older timestamp than the
	// snap assertions
	if modelAssertTime.IsZero() {
		modelAssertTime = s.snapDeclAssertsTime.Add(-30 * time.Minute)
	}

	s.sysLabel = "20191118"
	var kernel map[string]any
	if opts.hasKModsComps {
		kernel = map[string]any{
			"name":            "pc-kernel",
			"id":              testSeed.AssertedSnapID("pc-kernel"),
			"type":            "kernel",
			"default-channel": channel,
			"components": map[string]any{
				"kcomp1": "required",
				"kcomp2": "required",
				"kcomp3": map[string]any{
					"presence": "optional",
					"modes":    []any{"ephemeral"},
				},
			},
		}
	} else {
		kernel = map[string]any{
			"name":            "pc-kernel",
			"id":              testSeed.AssertedSnapID("pc-kernel"),
			"type":            "kernel",
			"default-channel": channel,
		}
	}
	model := map[string]any{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         base,
		"timestamp":    modelAssertTime.Format(time.RFC3339),
		"snaps": []any{
			kernel,
			map[string]any{
				"name":            "pc",
				"id":              testSeed.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": channel,
			},
			map[string]any{
				"name":            base,
				"id":              testSeed.AssertedSnapID(base),
				"type":            "base",
				"default-channel": "latest",
			},
		},
	}
	if s.isClassic {
		model["classic"] = "true"
		model["distribution"] = "ubuntu"
	}
	s.model = testSeed.MakeSeed(c, s.sysLabel, "my-brand", "my-model", model, nil)
}

// makeSnapFilesOnEarlyBootUbuntuData creates the snap files on ubuntu-data as
// we
func (s *baseInitramfsMountsSuite) makeSnapFilesOnEarlyBootUbuntuData(c *C, snaps ...snap.PlaceInfo) {
	snapDir := dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	if s.isClassic {
		snapDir = dirs.SnapBlobDirUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data"))
	}
	err := os.MkdirAll(snapDir, 0755)
	c.Assert(err, IsNil)
	for _, sn := range snaps {
		snFilename := sn.Filename()
		err = os.WriteFile(filepath.Join(snapDir, snFilename), nil, 0644)
		c.Assert(err, IsNil)
	}
}

func (s *baseInitramfsMountsSuite) mockProcCmdlineContent(c *C, newContent string) {
	mockProcCmdline := filepath.Join(c.MkDir(), "proc-cmdline")
	err := os.WriteFile(mockProcCmdline, []byte(newContent), 0644)
	c.Assert(err, IsNil)
	restore := kcmdline.MockProcCmdline(mockProcCmdline)
	s.AddCleanup(restore)
}

func (s *baseInitramfsMountsSuite) mockUbuntuSaveKeyAndMarker(c *C, rootDir, key, marker string) {
	keyPath := filepath.Join(dirs.SnapFDEDirUnder(rootDir), "ubuntu-save.key")
	c.Assert(os.MkdirAll(filepath.Dir(keyPath), 0700), IsNil)
	c.Assert(os.WriteFile(keyPath, []byte(key), 0600), IsNil)

	if marker != "" {
		markerPath := filepath.Join(dirs.SnapFDEDirUnder(rootDir), "marker")
		c.Assert(os.WriteFile(markerPath, []byte(marker), 0600), IsNil)
	}
}

func (s *baseInitramfsMountsSuite) mockUbuntuSaveMarker(c *C, rootDir, marker string) {
	markerPath := filepath.Join(rootDir, "device/fde", "marker")
	c.Assert(os.MkdirAll(filepath.Dir(markerPath), 0700), IsNil)
	c.Assert(os.WriteFile(markerPath, []byte(marker), 0600), IsNil)
}

type systemdMount struct {
	what  string
	where string
	opts  *main.SystemdMountOptions
	err   error
}

// this is a function so we evaluate InitramfsUbuntuBootDir, etc at the time of
// the test to pick up test-specific dirs.GlobalRootDir
func (s *baseInitramfsMountsSuite) ubuntuLabelMount(label string, mode string) systemdMount {
	mnt := systemdMount{
		opts: needsFsckDiskMountOpts,
	}
	switch label {
	case "ubuntu-boot":
		mnt.what = filepath.Join(s.byLabelDir, "ubuntu-boot")
		mnt.where = boot.InitramfsUbuntuBootDir
	case "ubuntu-seed":
		mnt.what = filepath.Join(s.byLabelDir, "ubuntu-seed")
		mnt.where = boot.InitramfsUbuntuSeedDir
		// don't fsck in run mode
		if mode == "run" {
			mnt.opts = needsNoSuidNoDevNoExecMountOpts
		} else {
			mnt.opts = needsFsckAndNoSuidNoDevNoExecMountOpts
		}
	case "ubuntu-data":
		mnt.what = filepath.Join(s.byLabelDir, "ubuntu-data")
		mnt.where = boot.InitramfsDataDir
		if s.isClassic {
			mnt.opts = needsFsckNoPrivateDiskMountOpts
		} else {
			mnt.opts = needsFsckAndNoSuidDiskMountOpts
		}
	}

	return mnt
}

// ubuntuPartUUIDMount returns a systemdMount for the partuuid disk, expecting
// that the partuuid contains in it the expected label for easier coding
func (s *baseInitramfsMountsSuite) ubuntuPartUUIDMount(partuuid string, mode string) systemdMount {
	// all partitions are expected to be mounted with fsck on
	mnt := systemdMount{
		opts: needsFsckDiskMountOpts,
	}
	mnt.what = filepath.Join("/dev/disk/by-partuuid", partuuid)
	switch {
	case strings.Contains(partuuid, "ubuntu-boot"):
		mnt.where = boot.InitramfsUbuntuBootDir
	case strings.Contains(partuuid, "ubuntu-seed"):
		mnt.where = boot.InitramfsUbuntuSeedDir
		mnt.opts = needsFsckAndNoSuidNoDevNoExecMountOpts
	case strings.Contains(partuuid, "ubuntu-data"):
		mnt.where = boot.InitramfsDataDir
		if s.isClassic {
			mnt.opts = needsFsckNoPrivateDiskMountOpts
		} else {
			mnt.opts = needsFsckAndNoSuidDiskMountOpts
		}
	case strings.Contains(partuuid, "ubuntu-save"):
		mnt.where = boot.InitramfsUbuntuSaveDir
		if mode == "run" {
			mnt.opts = needsFsckAndNoSuidNoDevNoExecMountOpts
		} else {
			mnt.opts = needsNoSuidNoDevNoExecMountOpts
		}
	}

	return mnt
}

func (s *baseInitramfsMountsSuite) makeSeedSnapSystemdMount(typ snap.Type) systemdMount {
	return s.makeSeedSnapSystemdMountForBase(typ, "core20")
}

func (s *baseInitramfsMountsSuite) makeSeedSnapSystemdMountForBase(typ snap.Type, base string) systemdMount {
	mnt := systemdMount{}
	var name, dir string
	switch typ {
	case snap.TypeSnapd:
		name = "snapd"
		dir = "snapd"
	case snap.TypeBase:
		name = base
		dir = "base"
	case snap.TypeGadget:
		name = "pc"
		dir = "gadget"
	case snap.TypeKernel:
		name = "pc-kernel"
		dir = "kernel"
	}
	mnt.what = filepath.Join(s.seedDir, "snaps", name+"_1.snap")
	mnt.where = filepath.Join(boot.InitramfsRunMntDir, dir)
	mnt.opts = snapMountOpts

	return mnt
}

func (s *baseInitramfsMountsSuite) makeRunSnapSystemdMount(typ snap.Type, sn snap.PlaceInfo) systemdMount {
	mnt := systemdMount{}
	var dir string
	switch typ {
	case snap.TypeSnapd:
		dir = "snapd"
	case snap.TypeBase:
		dir = "base"
	case snap.TypeGadget:
		dir = "gadget"
	case snap.TypeKernel:
		dir = "kernel"
	}

	snapDir := filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data")
	if s.isClassic {
		snapDir = filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/")
	}
	mnt.what = filepath.Join(dirs.SnapBlobDirUnder(snapDir), sn.Filename())
	mnt.where = filepath.Join(boot.InitramfsRunMntDir, dir)
	mnt.opts = snapMountOpts

	return mnt
}

func (s *baseInitramfsMountsSuite) mockSystemdMountSequence(c *C, mounts []systemdMount, comment CommentInterface) (restore func()) {
	n := 0
	if comment == nil {
		comment = Commentf("")
	}
	s.AddCleanup(func() {
		// make sure that after the test is done, we had as many mount calls as
		// mocked mounts
		c.Check(n, Equals, len(mounts), comment)
	})
	return main.MockSystemdMount(func(what, where string, opts *main.SystemdMountOptions) error {
		n++
		c.Assert(n <= len(mounts), Equals, true)
		if n > len(mounts) {
			return fmt.Errorf("unexpected systemd-mount call: %s, %s, %+v", what, where, opts)
		}
		mnt := mounts[n-1]
		c.Assert(what, Equals, mnt.what, comment)
		c.Assert(where, Equals, mnt.where, comment)
		c.Assert(opts, DeepEquals, mnt.opts, Commentf("what is %s, where is %s, comment is %s", what, where, comment))
		return mnt.err
	})
}

func (s *baseInitramfsMountsSuite) runInitramfsMountsUnencryptedTryRecovery(c *C, triedSystem bool) (err error) {
	s.mockProcCmdlineContent(c, "snapd_recovery_mode=recover  snapd_recovery_system="+s.sysLabel)

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()
	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}:     defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsHostUbuntuDataDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}:     defaultBootWithSaveDisk,
		},
	)
	defer restore()
	restore = s.mockSystemdMountSequence(c, []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", "recover"),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-data-partuuid",
			boot.InitramfsHostUbuntuDataDir,
			needsNoSuidDiskMountOpts,
			nil,
		},
		{
			"/dev/disk/by-partuuid/ubuntu-save-partuuid",
			boot.InitramfsUbuntuSaveDir,
			needsNoSuidNoDevNoExecMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	if triedSystem {
		defer func() {
			err = recover().(error)
		}()
	}

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	checkSnapdMountUnit(c)
	return err
}

func (s *baseInitramfsMountsSuite) testInitramfsMountsTryRecoveryHappy(c *C, happyStatus string) {
	rebootCalls := 0
	restore := boot.MockInitramfsReboot(func() error {
		rebootCalls++
		return nil
	})
	defer restore()

	bl := bootloadertest.Mock("bootloader", c.MkDir())
	bl.BootVars = map[string]string{
		"recovery_system_status": happyStatus,
		"try_recovery_system":    s.sysLabel,
	}
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	hostUbuntuData := filepath.Join(boot.InitramfsRunMntDir, "host/ubuntu-data/")
	var mockedState string
	if s.isClassic {
		mockedState = filepath.Join(hostUbuntuData, "var/lib/snapd/state.json")
	} else {
		mockedState = filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	}
	c.Assert(os.MkdirAll(filepath.Dir(mockedState), 0750), IsNil)
	c.Assert(os.WriteFile(mockedState, []byte(mockStateContent), 0640), IsNil)

	const triedSystem = true
	err := s.runInitramfsMountsUnencryptedTryRecovery(c, triedSystem)
	// due to hackery with replacing reboot, we expect a non nil error that
	// actually indicates a success
	c.Assert(err, ErrorMatches, `finalize try recovery system did not reboot, last error: <nil>`)

	// modeenv is not written as reboot happens before that
	var modeEnv string
	if s.isClassic {
		modeEnv = dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data"))
	} else {
		modeEnv = dirs.SnapModeenvFileUnder(filepath.Join(dirs.GlobalRootDir, "/run/mnt/data/system-data"))
	}
	c.Check(modeEnv, testutil.FileAbsent)
	c.Check(bl.BootVars, DeepEquals, map[string]string{
		"recovery_system_status": "tried",
		"try_recovery_system":    s.sysLabel,
		"snapd_recovery_mode":    "run",
		"snapd_recovery_system":  "",
	})
	c.Check(rebootCalls, Equals, 1)
}

type initramfsMountsSuite struct {
	baseInitramfsMountsSuite
}

var _ = Suite(&initramfsMountsSuite{})

// static test cases for time test variants shared across the different modes

type timeTestCase struct {
	now          time.Time
	modelTime    time.Time
	expT         time.Time
	setTimeCalls int
	comment      string
}

func (s *initramfsMountsSuite) timeTestCases() []timeTestCase {
	// epoch time
	epoch := time.Time{}

	// t1 is the kernel initrd build time
	t1 := s.snapDeclAssertsTime.Add(-30 * 24 * time.Hour)
	// technically there is another time here between t1 and t2, that is the
	// default model sign time, but since it's older than the snap assertion
	// sign time (t2) it's not actually used in the test

	// t2 is the time that snap-revision / snap-declaration assertions will be
	// signed with
	t2 := s.snapDeclAssertsTime

	// t3 is a time after the snap-declarations are signed
	t3 := s.snapDeclAssertsTime.Add(30 * 24 * time.Hour)

	// t4 and t5 are both times after the the snap declarations are signed
	t4 := s.snapDeclAssertsTime.Add(60 * 24 * time.Hour)
	t5 := s.snapDeclAssertsTime.Add(120 * 24 * time.Hour)

	return []timeTestCase{
		{
			now:          epoch,
			expT:         t2,
			setTimeCalls: 1,
			comment:      "now() is epoch",
		},
		{
			now:          t1,
			expT:         t2,
			setTimeCalls: 1,
			comment:      "now() is kernel initrd sign time",
		},
		{
			now:          t3,
			expT:         t3,
			setTimeCalls: 0,
			comment:      "now() is newer than snap assertion",
		},
		{
			now:          t3,
			modelTime:    t4,
			expT:         t4,
			setTimeCalls: 1,
			comment:      "model time is newer than now(), which is newer than snap asserts",
		},
		{
			now:          t5,
			modelTime:    t4,
			expT:         t5,
			setTimeCalls: 0,
			comment:      "model time is newest, but older than now()",
		},
	}
}

func (s *initramfsMountsSuite) testRecoverModeHappy(c *C, base string) {
	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	restore := main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return nil
	})
	defer restore()

	// mock various files that are copied around during recover mode (and files
	// that shouldn't be copied around)
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
		"system-data/etc/machine-id", // machine-id for systemd-networkd
	}
	mockUnrelatedFiles := []string{
		"system-data/var/lib/foo",
		"system-data/etc/passwd",
		"user-data/user1/some-random-data",
		"user-data/user2/other-random-data",
		"user-data/user2/.snap/sneaky-not-auth.json",
		"system-data/etc/not-networking/netplan",
		"system-data/var/lib/systemd/timesync/clock-not-the-clock",
		"system-data/etc/machine-id-except-not",
	}
	for _, mockFile := range append(mockCopiedFiles, mockUnrelatedFiles...) {
		p := filepath.Join(hostUbuntuData, mockFile)
		err = os.MkdirAll(filepath.Dir(p), 0750)
		c.Assert(err, IsNil)
		mockContent := fmt.Sprintf("content of %s", filepath.Base(mockFile))
		err = os.WriteFile(p, []byte(mockContent), 0640)
		c.Assert(err, IsNil)
	}
	// create a mock state
	mockedState := filepath.Join(hostUbuntuData, "system-data/var/lib/snapd/state.json")
	err = os.MkdirAll(filepath.Dir(mockedState), 0750)
	c.Assert(err, IsNil)
	err = os.WriteFile(mockedState, []byte(mockStateContent), 0640)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)

	// we always need to lock access to sealed keys
	c.Check(sealedKeysLocked, Equals, true)

	modeEnv := filepath.Join(ephemeralUbuntuData, "/system-data/var/lib/snapd/modeenv")
	c.Check(modeEnv, testutil.FileEquals, `mode=recover
recovery_system=20191118
base=`+base+`_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
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

	c.Check(filepath.Join(ephemeralUbuntuData, "system-data/var/lib/snapd/state.json"), testutil.FileEquals, `{"data":{"auth":{"last-id":1,"macaroon-key":"not-a-cookie","users":[{"id":1,"name":"mvo"}]}},"changes":{},"tasks":{},"last-change-id":0,"last-task-id":0,"last-lane-id":0,"last-notice-id":0}`)

	// finally check that the recovery system bootenv was updated to be in run
	// mode
	bloader, err := bootloader.Find("", nil)
	c.Assert(err, IsNil)
	m, err := bloader.GetBootVars("snapd_recovery_system", "snapd_recovery_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191118",
		"snapd_recovery_mode":   "run",
	})
}

func (s *initramfsMountsSuite) testInitramfsMountsInstallRecoverModeMeasure(c *C, mode string) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, s.sysLabel))

	modeMnts := []systemdMount{
		s.ubuntuLabelMount("ubuntu-seed", mode),
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
		},
	}

	mockDiskMapping := map[disks.Mountpoint]*disks.MockDiskMapping{
		{Mountpoint: boot.InitramfsUbuntuSeedDir}: {
			Structure: []disks.Partition{
				seedPart,
			},
			DiskHasPartitions: true,
		},
	}

	if mode == "recover" {
		// setup a bootloader for setting the bootenv after we are done
		bloader := bootloadertest.Mock("mock", c.MkDir())
		bootloader.Force(bloader)
		defer bootloader.Force(nil)

		// add the expected mount of ubuntu-data onto the host data dir
		modeMnts = append(modeMnts,
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-boot-partuuid",
				boot.InitramfsUbuntuBootDir,
				needsFsckDiskMountOpts,
				nil,
			},
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-data-partuuid",
				boot.InitramfsHostUbuntuDataDir,
				needsNoSuidDiskMountOpts,
				nil,
			},
			systemdMount{
				"/dev/disk/by-partuuid/ubuntu-save-partuuid",
				boot.InitramfsUbuntuSaveDir,
				needsNoSuidNoDevNoExecMountOpts,
				nil,
			})

		// also add the ubuntu-data and ubuntu-save fs labels to the
		// disk referenced by the ubuntu-seed partition
		disk := mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuSeedDir}]
		disk.Structure = append(disk.Structure, bootPart, savePart, dataPart)

		// and also add the /run/mnt/host/ubuntu-{boot,data,save} mountpoints
		// for cross-checking after mounting
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuBootDir}] = disk
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsHostUbuntuDataDir}] = disk
		mockDiskMapping[disks.Mountpoint{Mountpoint: boot.InitramfsUbuntuSaveDir}] = disk
	}

	restore := disks.MockMountPointDisksToPartitionMapping(mockDiskMapping)
	defer restore()

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

	restore = s.mockSystemdMountSequence(c, modeMnts, nil)
	defer restore()

	if mode == "recover" {
		// use the helper
		s.testRecoverModeHappy(c, "core20")
	} else {
		_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
		c.Assert(err, IsNil)

		modeEnv := filepath.Join(boot.InitramfsDataDir, "/system-data/var/lib/snapd/modeenv")
		c.Check(modeEnv, testutil.FileEquals, `mode=install
recovery_system=20191118
base=core20_1.snap
gadget=pc_1.snap
model=my-brand/my-model
grade=signed
`)
	}

	c.Check(measuredModel, NotNil)
	c.Check(measuredModel, DeepEquals, s.model)
	c.Check(measureEpochCalls, Equals, 1)
	c.Check(measureModelCalls, Equals, 1)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, s.sysLabel+"-model-measured"), testutil.FilePresent)

	checkSnapdMountUnit(c)
}

func (s *initramfsMountsSuite) testInitramfsMountsEncryptedNoModel(c *C, mode, label string, expectedMeasureModelCalls int) {
	s.mockProcCmdlineContent(c, fmt.Sprintf("snapd_recovery_mode=%s", mode))

	// Make sure there is no model for this test
	err := os.Remove(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"))
	c.Assert(err, IsNil)

	// ensure that we check that access to sealed keys were locked
	sealedKeysLocked := false
	defer main.MockSecbootLockSealedKeys(func() error {
		sealedKeysLocked = true
		return fmt.Errorf("blocking keys failed")
	})()

	// in install mode we fail before any mount happens
	// in install / recover mode the code doesn't make it far enough to do
	// any disk cross checking
	switch mode {
	case "run":
		// run mode will mount ubuntu-boot only before failing
		restore := s.mockSystemdMountSequence(c, []systemdMount{
			s.ubuntuLabelMount("ubuntu-boot", mode),
		}, nil)
		defer restore()
		restore = disks.MockMountPointDisksToPartitionMapping(
			map[disks.Mountpoint]*disks.MockDiskMapping{
				{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultEncBootDisk,
			},
		)
		defer restore()
	default:
		// install and recover mounts are just ubuntu-seed before we fail
		restore := s.mockSystemdMountSequence(c, []systemdMount{
			s.ubuntuLabelMount("ubuntu-seed", mode),
		}, nil)

		// in install / recover mode the code doesn't make it far enough to do
		// any disk cross checking
		defer restore()
	}

	if label != "" {
		s.mockProcCmdlineContent(c,
			fmt.Sprintf("snapd_recovery_mode=%s snapd_recovery_system=%s", mode, label))
		// break the seed
		err := os.Remove(filepath.Join(s.seedDir, "systems", label, "model"))
		c.Assert(err, IsNil)
	}

	measureEpochCalls := 0
	restore := main.MockSecbootMeasureSnapSystemEpochWhenPossible(func() error {
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

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	where := "/run/mnt/ubuntu-boot/device/model"
	if mode != "run" {
		where = fmt.Sprintf("/run/mnt/ubuntu-seed/systems/%s/model", label)
	}
	c.Assert(err, ErrorMatches, fmt.Sprintf(".*cannot read model assertion: open .*%s: no such file or directory", where))
	c.Assert(measureEpochCalls, Equals, 1)
	c.Assert(measureModelCalls, Equals, expectedMeasureModelCalls)
	c.Assert(filepath.Join(dirs.SnapBootstrapRunDir, "secboot-epoch-measured"), testutil.FilePresent)
	gl, err := filepath.Glob(filepath.Join(dirs.SnapBootstrapRunDir, "*-model-measured"))
	c.Assert(err, IsNil)
	c.Assert(gl, HasLen, 0)
	c.Check(sealedKeysLocked, Equals, true)
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

func (s *initramfsMountsSuite) TestMountNonDataPartitionPolls(c *C) {
	restore := main.MockPartitionUUIDForBootedKernelDisk("some-uuid")
	defer restore()

	var waitFile []string
	var pollWait time.Duration
	var pollIterations int
	restore = main.MockWaitFile(func(path string, wait time.Duration, n int) error {
		waitFile = append(waitFile, path)
		pollWait = wait
		pollIterations = n
		return fmt.Errorf("error")
	})
	defer restore()

	n := 0
	restore = main.MockSystemdMount(func(what, where string, opts *main.SystemdMountOptions) error {
		n++
		return nil
	})
	defer restore()

	err := main.MountNonDataPartitionMatchingKernelDisk("/some/target", "", &main.SystemdMountOptions{})
	c.Check(err, ErrorMatches, "cannot find device: error")
	c.Check(n, Equals, 0)
	c.Check(waitFile, DeepEquals, []string{
		filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid/some-uuid"),
	})
	c.Check(pollWait, DeepEquals, 50*time.Millisecond)
	c.Check(pollIterations, DeepEquals, 1200)
	c.Check(s.logs.String(), Matches, "(?m).* waiting up to 1m0s for /dev/disk/by-partuuid/some-uuid to appear")
	// there is only a single log msg
	c.Check(strings.Count(s.logs.String(), "\n"), Equals, 1)
}

func (s *initramfsMountsSuite) TestMountNonDataPartitionNoPollNoLogMsg(c *C) {
	restore := main.MockPartitionUUIDForBootedKernelDisk("some-uuid")
	defer restore()

	n := 0
	restore = main.MockSystemdMount(func(what, where string, opts *main.SystemdMountOptions) error {
		n++
		return nil
	})
	defer restore()

	fakedPartSrc := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partuuid/some-uuid")
	err := os.MkdirAll(filepath.Dir(fakedPartSrc), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedPartSrc, nil, 0644)
	c.Assert(err, IsNil)

	err = main.MountNonDataPartitionMatchingKernelDisk("some-target", "", &main.SystemdMountOptions{})
	c.Check(err, IsNil)
	c.Check(s.logs.String(), Equals, "")
	c.Check(n, Equals, 1)
}

func (s *initramfsMountsSuite) TestWaitFileErr(c *C) {
	err := main.WaitFile("/dev/does-not-exist", 10*time.Millisecond, 2)
	c.Check(err, ErrorMatches, "no /dev/does-not-exist after waiting for 20ms")
}

func (s *initramfsMountsSuite) TestWaitFile(c *C) {
	existingPartSrc := filepath.Join(c.MkDir(), "does-exist")
	err := os.WriteFile(existingPartSrc, nil, 0644)
	c.Assert(err, IsNil)

	err = main.WaitFile(existingPartSrc, 5000*time.Second, 1)
	c.Check(err, IsNil)

	err = main.WaitFile(existingPartSrc, 1*time.Second, 10000)
	c.Check(err, IsNil)
}

func (s *initramfsMountsSuite) TestWaitFileWorksWithFilesAppearingLate(c *C) {
	eventuallyExists := filepath.Join(c.MkDir(), "eventually-exists")
	go func() {
		time.Sleep(40 * time.Millisecond)
		err := os.WriteFile(eventuallyExists, nil, 0644)
		c.Assert(err, IsNil)
	}()

	err := main.WaitFile(eventuallyExists, 5*time.Millisecond, 1000)
	c.Check(err, IsNil)
}

func (s *initramfsMountsSuite) TestGetDiskNotUEFINotKernelCmdlineOk(c *C) {
	mockUdevadm := testutil.MockCommand(c, "udevadm", `
	echo "ID_FS_TYPE=vfat"
`)
	defer mockUdevadm.Restore()

	err := os.Remove(filepath.Join(s.byLabelDir, "ubuntu-seed"))
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-SEED"), nil, 0644)
	c.Assert(err, IsNil)

	path, err := main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err, IsNil)
	c.Assert(path, Equals, filepath.Join(s.byLabelDir, "UBUNTU-SEED"))

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name",
			filepath.Join(s.byLabelDir, "UBUNTU-SEED")},
	})
}

func (s *initramfsMountsSuite) TestGetDiskNotUEFINotKernelCmdlineSomeItersOk(c *C) {
	mockUdevadm := testutil.MockCommand(c, "udevadm", `
	echo "ID_FS_TYPE=vfat"
`)
	defer mockUdevadm.Restore()
	s.AddCleanup(main.MockPollWaitForLabel(50 * time.Millisecond))
	s.AddCleanup(main.MockPollWaitForLabelIters(100))

	err := os.Remove(filepath.Join(s.byLabelDir, "ubuntu-seed"))
	c.Assert(err, IsNil)

	ch := make(chan bool)
	go func() {
		path, err := main.GetNonUEFISystemDisk("ubuntu-seed")
		c.Check(err, IsNil)
		c.Check(path, Equals, filepath.Join(s.byLabelDir, "UBUNTU-SEED"))
		ch <- true
	}()
	// Wait a bit so we get at least an iteration
	time.Sleep(50 * time.Millisecond)
	// Now create a file that matches the label
	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-SEED"), nil, 0644)
	c.Assert(err, IsNil)

	<-ch

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name",
			filepath.Join(s.byLabelDir, "UBUNTU-SEED")},
	})
}

func (s *initramfsMountsSuite) TestGetDiskNotUEFINotKernelCmdlineFailNoFs(c *C) {
	mockUdevadm := testutil.MockCommand(c, "udevadm", `
`)
	defer mockUdevadm.Restore()

	err := os.Remove(filepath.Join(s.byLabelDir, "ubuntu-seed"))
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.byLabelDir, "UBUNTU-SEED"), nil, 0644)
	c.Assert(err, IsNil)

	path, err := main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err.Error(), Equals, `no candidate found for label "ubuntu-seed" ("UBUNTU-SEED" is not vfat)`)
	c.Assert(path, Equals, "")

	c.Assert(mockUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "info", "--query", "property", "--name",
			filepath.Join(s.byLabelDir, "UBUNTU-SEED")},
	})
}

func (s *initramfsMountsSuite) TestGetDiskNotUEFISeedPartCapitalFsLabel(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/dev/sda snapd_recovery_mode=run")

	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": defaultBootWithSeedPartCapitalFsLabel,
	})
	defer restore()

	path, err := main.GetNonUEFISystemDisk("ubuntu-seed")
	c.Assert(err, IsNil)
	c.Assert(path, Equals, "/dev/sda2")

	path, err = main.GetNonUEFISystemDisk("UBUNTU-SEED")
	c.Assert(err, IsNil)
	c.Assert(path, Equals, "/dev/sda2")

	path, err = main.GetNonUEFISystemDisk("ubuntu-boot")
	c.Assert(err, IsNil)
	c.Assert(path, Equals, "/dev/sda3")

	path, err = main.GetNonUEFISystemDisk("UBUNTU-BOOT")
	c.Assert(err.Error(), Equals, `filesystem label "UBUNTU-BOOT" not found`)
	c.Assert(path, Equals, "")
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsObeyDevLink(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/should/be/ignored snapd_recovery_mode=run")

	devLink := filepath.Join(dirs.GlobalRootDir, "/dev/disk/snapd/disk")
	c.Assert(os.MkdirAll(filepath.Dir(devLink), 0755), IsNil)
	fakeDevice := filepath.Join(dirs.GlobalRootDir, "/dev/sda")
	c.Assert(os.WriteFile(fakeDevice, []byte{}, 0644), IsNil)
	c.Assert(os.Symlink(fakeDevice, devLink), IsNil)

	restoreDiskMapping := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": defaultBootWithSaveDisk,
	})
	defer restoreDiskMapping()

	restore := main.MockPartitionUUIDForBootedKernelDisk("ubuntu-boot-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/sda3",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsObeyDevLinkFallback(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/should/be/ignored snapd_recovery_mode=run")

	devLink := filepath.Join(dirs.GlobalRootDir, "/dev/disk/snapd/disk")
	c.Assert(os.MkdirAll(filepath.Dir(devLink), 0755), IsNil)
	fakeDevice := filepath.Join(dirs.GlobalRootDir, "/dev/sda")
	c.Assert(os.WriteFile(fakeDevice, []byte{}, 0644), IsNil)
	c.Assert(os.Symlink(fakeDevice, devLink), IsNil)

	restoreDiskMapping := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": defaultBootWithSaveDisk,
	})
	defer restoreDiskMapping()

	// NO UEFI
	restore := main.MockPartitionUUIDForBootedKernelDisk("")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuBootDir}: defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsDataDir}:       defaultBootWithSaveDisk,
			{Mountpoint: boot.InitramfsUbuntuSaveDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/sda3",
			boot.InitramfsUbuntuBootDir,
			needsFsckDiskMountOpts,
			nil,
		},
		s.ubuntuPartUUIDMount("ubuntu-seed-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-data-partuuid", "run"),
		s.ubuntuPartUUIDMount("ubuntu-save-partuuid", "run"),
		s.makeRunSnapSystemdMount(snap.TypeGadget, s.gadget),
		s.makeRunSnapSystemdMount(snap.TypeKernel, s.kernel),
	}, nil)
	defer restore()

	// mock a bootloader
	bloader := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// set the current kernel
	restore = bloader.SetEnabledKernel(s.kernel)
	defer restore()

	writeGadget(c, "ubuntu-seed", "system-seed", "")

	s.makeSnapFilesOnEarlyBootUbuntuData(c, s.kernel, s.core20, s.gadget)

	// write modeenv
	modeEnv := boot.Modeenv{
		Mode:           "run",
		Base:           s.core20.Filename(),
		Gadget:         s.gadget.Filename(),
		CurrentKernels: []string{s.kernel.Filename()},
	}
	err := modeEnv.WriteTo(boot.InitramfsDataDir)
	c.Assert(err, IsNil)

	_, err = main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}

func (s *initramfsClassicMountsSuite) TestInitramfsMountsInstallObeyDevLink(c *C) {
	s.mockProcCmdlineContent(c, "snapd_system_disk=/should/be/ignored snapd_recovery_mode=install snapd_recovery_system="+s.sysLabel)

	devLink := filepath.Join(dirs.GlobalRootDir, "/dev/disk/snapd/disk")
	c.Assert(os.MkdirAll(filepath.Dir(devLink), 0755), IsNil)
	fakeDevice := filepath.Join(dirs.GlobalRootDir, "/dev/sda")
	c.Assert(os.WriteFile(fakeDevice, []byte{}, 0644), IsNil)
	c.Assert(os.Symlink(fakeDevice, devLink), IsNil)

	restoreDiskMapping := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": defaultBootWithSaveDisk,
	})
	defer restoreDiskMapping()

	restore := main.MockPartitionUUIDForBootedKernelDisk("ubuntu-seed-partuuid")
	defer restore()

	restore = disks.MockMountPointDisksToPartitionMapping(
		map[disks.Mountpoint]*disks.MockDiskMapping{
			{Mountpoint: boot.InitramfsUbuntuSeedDir}: defaultBootWithSaveDisk,
		},
	)
	defer restore()

	restore = s.mockSystemdMountSequence(c, []systemdMount{
		{
			"/dev/sda2",
			boot.InitramfsUbuntuSeedDir,
			needsFsckAndNoSuidNoDevNoExecMountOpts,
			nil,
		},
		s.makeSeedSnapSystemdMount(snap.TypeKernel),
		s.makeSeedSnapSystemdMount(snap.TypeBase),
		s.makeSeedSnapSystemdMount(snap.TypeGadget),
		{
			"tmpfs",
			boot.InitramfsDataDir,
			tmpfsMountOpts,
			nil,
		},
	}, nil)
	defer restore()

	_, err := main.Parser().ParseArgs([]string{"initramfs-mounts"})
	c.Assert(err, IsNil)
}
