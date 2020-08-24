// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type assetsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&assetsSuite{})

func checkContentGlob(c *C, glob string, expected []string) {
	l, err := filepath.Glob(glob)
	c.Assert(err, IsNil)
	c.Check(l, DeepEquals, expected)
}

func (s *assetsSuite) TestAssetsCacheAddRemove(c *C) {
	cacheDir := c.MkDir()
	d := c.MkDir()

	cache := boot.NewTrustedAssetsCache(cacheDir)

	data := []byte("foobar")
	// SHA3-384
	hash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	// add a new file
	ta, err := cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
	c.Assert(err, IsNil)
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)), testutil.FileEquals, string(data))
	c.Check(ta, NotNil)

	// try the same file again
	taAgain, err := cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
	c.Assert(err, IsNil)
	// file already cached
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)), testutil.FileEquals, string(data))
	// and there's just one entry in the cache
	checkContentGlob(c, filepath.Join(cacheDir, "grub", "*"), []string{
		filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)),
	})
	// let go-check do the deep equals check
	c.Check(taAgain, DeepEquals, ta)

	// same data but different asset name
	taDifferentAsset, err := cache.Add(filepath.Join(d, "foobar"), "grub", "bootx64.efi")
	c.Assert(err, IsNil)
	// new entry in cache
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)), testutil.FileEquals, string(data))
	// 2 files now
	checkContentGlob(c, filepath.Join(cacheDir, "grub", "*"), []string{
		filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)),
		filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)),
	})
	c.Check(taDifferentAsset, NotNil)

	// same source, data (new hash), existing asset name
	newData := []byte("new foobar")
	newHash := "5aa87615f6613a37d63c9a29746ef57457286c37148a4ae78493b0face5976c1fea940a19486e6bef65d43aec6b8f5a2"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), newData, 0644)
	c.Assert(err, IsNil)

	taExistingAssetName, err := cache.Add(filepath.Join(d, "foobar"), "grub", "bootx64.efi")
	c.Assert(err, IsNil)
	// new entry in cache
	c.Check(taExistingAssetName, NotNil)
	// we have both new and old asset
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", newHash)), testutil.FileEquals, string(newData))
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)), testutil.FileEquals, string(data))
	// 3 files in total
	checkContentGlob(c, filepath.Join(cacheDir, "grub", "*"), []string{
		filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)),
		filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", newHash)),
		filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)),
	})

	// drop
	err = cache.Remove("grub", "bootx64.efi", newHash)
	c.Assert(err, IsNil)
	// asset bootx64.efi with given hash was dropped
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", newHash)), testutil.FileAbsent)
	// the other file still exists
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)), testutil.FileEquals, string(data))
	// remove it too
	err = cache.Remove("grub", "bootx64.efi", hash)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("bootx64.efi-%s", hash)), testutil.FileAbsent)

	// what is left is the grub assets only
	checkContentGlob(c, filepath.Join(cacheDir, "grub", "*"), []string{
		filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", hash)),
	})
}

func (s *assetsSuite) TestAssetsCacheAddErr(c *C) {
	cacheDir := c.MkDir()
	d := c.MkDir()
	cache := boot.NewTrustedAssetsCache(cacheDir)

	defer os.Chmod(cacheDir, 0755)
	err := os.Chmod(cacheDir, 0000)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foo"), 0644)
	c.Assert(err, IsNil)
	// cannot create bootloader subdirectory
	ta, err := cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
	c.Assert(err, ErrorMatches, "cannot create cache directory: mkdir .*/grub: permission denied")
	c.Check(ta, IsNil)

	// fix it now
	err = os.Chmod(cacheDir, 0755)
	c.Assert(err, IsNil)

	_, err = cache.Add(filepath.Join(d, "no-file"), "grub", "grubx64.efi")
	c.Assert(err, ErrorMatches, "cannot open asset file: open .*/no-file: no such file or directory")

	blDir := filepath.Join(cacheDir, "grub")
	defer os.Chmod(blDir, 0755)
	err = os.Chmod(blDir, 0000)
	c.Assert(err, IsNil)

	_, err = cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
	c.Assert(err, ErrorMatches, `cannot create temporary cache file: open .*/grub/grubx64\.efi\.temp\.[a-zA-Z0-9]+~: permission denied`)
}

func (s *assetsSuite) TestAssetsCacheRemoveErr(c *C) {
	cacheDir := c.MkDir()
	d := c.MkDir()
	cache := boot.NewTrustedAssetsCache(cacheDir)

	data := []byte("foobar")
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	// cannot create bootloader subdirectory
	_, err = cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
	c.Assert(err, IsNil)
	// sanity
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", dataHash)), testutil.FileEquals, string(data))

	err = cache.Remove("grub", "no file", "some-hash")
	c.Assert(err, IsNil)

	// different asset name but known hash
	err = cache.Remove("grub", "different-name", dataHash)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(cacheDir, "grub", fmt.Sprintf("grubx64.efi-%s", dataHash)), testutil.FileEquals, string(data))
}

func (s *assetsSuite) TestInstallObserverNew(c *C) {
	d := c.MkDir()
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	// but nil for non UC20
	nonUC20Model := makeMockModel()
	nonUC20obs, err := boot.TrustedAssetsInstallObserverForModel(nonUC20Model, d)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)
}

var (
	mockRunBootStruct = &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemBoot,
		},
	}
	mockSeedStruct = &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemSeed,
		},
	}
)

func (s *assetsSuite) TestInstallObserverObserveSystemBootRealGrub(c *C) {
	d := c.MkDir()

	// mock a bootloader that uses trusted assets
	err := ioutil.WriteFile(filepath.Join(d, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	otherData := []byte("other foobar")
	err = ioutil.WriteFile(filepath.Join(d, "other-foobar"), otherData, 0644)
	c.Assert(err, IsNil)

	// only grubx64.efi gets installed to system-boot
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	// Observe is called when populating content, but one can freely specify
	// overlapping content entries, so a same file may be observed more than
	// once
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	// try with one more file, which is not a trusted asset of a run mode, so it is ignored
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "EFI/boot/bootx64.efi")
	c.Assert(err, IsNil)
	// a single file in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "grub", fmt.Sprintf("grubx64.efi-%s", dataHash)),
	})

	// and one more, a non system-boot structure, so the file is ignored
	systemSeedStruct := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemSeed,
		},
	}
	_, err = obs.Observe(gadget.ContentWrite, systemSeedStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "other-foobar"), "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	// still, only one entry in the cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "grub", fmt.Sprintf("grubx64.efi-%s", dataHash)),
	})

	// let's see what the observer has tracked
	tracked := obs.CurrentTrustedBootAssetsMap()
	c.Check(tracked, DeepEquals, boot.BootAssetsMap{
		"grubx64.efi": []string{dataHash},
	})
}

func (s *assetsSuite) TestInstallObserverObserveSystemBootMocked(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("trusted-assets", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"nested/other-asset",
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	// observe same asset again
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	// different one
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "nested/other-asset")
	c.Assert(err, IsNil)
	// a non trusted asset
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "non-trusted")
	c.Assert(err, IsNil)
	// a single file in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted-assets", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted-assets", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted-assets", fmt.Sprintf("other-asset-%s", dataHash)),
	})
	// the list of trusted assets was asked for just once
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// let's see what the observer has tracked
	tracked := obs.CurrentTrustedBootAssetsMap()
	c.Check(tracked, DeepEquals, boot.BootAssetsMap{
		"asset":       []string{dataHash},
		"other-asset": []string{dataHash},
	})
}

func (s *assetsSuite) TestInstallObserverNonTrustedBootloader(c *C) {
	d := c.MkDir()

	// MockBootloader does not implement trusted assets
	bootloader.Force(bootloadertest.Mock("mock", ""))
	defer bootloader.Force(nil)

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)
	// bootloder is found, but ignored because it does not support trusted assets
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	c.Check(osutil.IsDirectory(dirs.SnapBootAssetsDir), Equals, false,
		Commentf("%q exists while it should not", dirs.SnapBootAssetsDir))
	c.Check(obs.CurrentTrustedBootAssetsMap(), IsNil)
}

func (s *assetsSuite) TestInstallObserverTrustedButNoAssets(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("trusted-assets", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)
	// bootloder is found, but ignored because it does not support trusted assets
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "other-asset")
	c.Assert(err, IsNil)
	// the list of trusted assets was asked for just once
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	c.Check(obs.CurrentTrustedBootAssetsMap(), IsNil)
}

func (s *assetsSuite) TestInstallObserverObserveErr(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("trusted-assets", "").WithTrustedAssets()
	tab.TrustedAssetsErr = fmt.Errorf("mocked trusted assets error")

	bootloader.ForceError(fmt.Errorf("mocked bootloader error"))
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("data"), 0644)
	c.Assert(err, IsNil)

	// there is no known bootloader in gadget
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, "cannot find bootloader: mocked bootloader error")

	// force a bootloader now
	bootloader.ForceError(nil)
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot list "trusted-assets" bootloader trusted assets: mocked trusted assets error`)
}

func (s *assetsSuite) TestUpdateObserverNew(c *C) {
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	// but nil for non UC20
	nonUC20Model := makeMockModel()
	nonUC20obs, err := boot.TrustedAssetsUpdateObserverForModel(nonUC20Model)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)
}

func (s *assetsSuite) TestUpdateObserverUpdateMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"asset-hash"},
			"shim":  {"shim-hash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recovery-asset-hash"},
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"nested/other-asset",
		"shim",
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "shim"), "shim")
	c.Assert(err, IsNil)
	// the list of trusted assets was asked once for the boot bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// observe the recovery struct
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "shim"), "shim")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "nested/other-asset")
	c.Assert(err, IsNil)
	// and once again for the recovery bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// all files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("other-asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"asset-hash", dataHash},
		"shim":  {"shim-hash", shimHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset":       {"recovery-asset-hash", dataHash},
		"shim":        {shimHash},
		"other-asset": {dataHash},
	})
}

func (s *assetsSuite) TestUpdateObserverUpdateExistingAssetMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"shim",
	}

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	// add one file to the cache, as if the system got rebooted before
	// modeenv got updated
	cache := boot.NewTrustedAssetsCache(dirs.SnapBootAssetsDir)
	_, err = cache.Add(filepath.Join(d, "foobar"), "trusted", "asset")
	c.Assert(err, IsNil)
	// file is in the cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
	})

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"asset-hash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			// shim with same hash is listed as trusted, but missing
			// from cache
			"shim": {shimHash},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// observe the updates
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "shim"), "shim")
	c.Assert(err, IsNil)
	// trusted assets were asked for
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// file is in the cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		// shim was added to cache
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"asset-hash", dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
}

func (s *assetsSuite) TestUpdateObserverUpdateNothingTrackedMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
	}

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	m := boot.Modeenv{
		Mode: "run",
		// nothing is tracked in modeenv yet
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// observe the updates
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	// trusted assets were asked for
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// file is in the cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
}

func (s *assetsSuite) TestUpdateObserverUpdateOtherRoleStructMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{"asset"}

	// modeenv is not set up, but the observer should not care

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// non system-boot or system-seed structure gets ignored
	mockVolumeStruct := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemData,
		},
	}

	// observe the updates
	_, err = obs.Observe(gadget.ContentUpdate, mockVolumeStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	// trusted assets were asked for
	c.Check(tab.TrustedAssetsCalls, Equals, 0)
}

func (s *assetsSuite) TestUpdateObserverUpdateNotTrustedMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	// mot a non trusted assets bootloader
	bl := bootloadertest.Mock("not-trusted", "")
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	err := ioutil.WriteFile(filepath.Join(d, "foobar"), nil, 0644)
	c.Assert(err, IsNil)

	// no need to mock modeenv, the bootloader has no trusted assets

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// observe the updates
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
}

func (s *assetsSuite) TestUpdateObserverUpdateTrivialErr(c *C) {
	// test trivial error scenarios of the update observer

	d := c.MkDir()
	root := c.MkDir()

	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// first no bootloader
	bootloader.ForceError(fmt.Errorf("bootloader fail"))

	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, "cannot find bootloader: bootloader fail")
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, "cannot find bootloader: bootloader fail")

	bootloader.ForceError(nil)
	bl := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	bl.TrustedAssetsList = []string{"asset"}
	bl.TrustedAssetsErr = fmt.Errorf("fail")

	// listing trusted assets fails
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot list "trusted" bootloader trusted assets: fail`)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot list "trusted" bootloader trusted assets: fail`)

	bl.TrustedAssetsErr = nil

	// no modeenv
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot load modeenv: .* no such file or directory`)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot load modeenv: .* no such file or directory`)

	m := boot.Modeenv{
		Mode: "run",
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// no source file, hash will fail
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot open asset file: .*/foobar: no such file or directory`)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot open asset file: .*/foobar: no such file or directory`)
}

func (s *assetsSuite) TestUpdateObserverUpdateRepeatedAssetErr(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	bl := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	bl.TrustedAssetsList = []string{"asset"}

	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// we are already tracking 2 assets, this is an unexpected state for observing content updates
	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"one", "two"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"one", "two"},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// and the source file
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), nil, 0644)
	c.Assert(err, IsNil)

	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, filepath.Join(d, "foobar"), "asset")
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
}

func (s *assetsSuite) TestUpdateObserverRollbackModeenvManipulationMocked(c *C) {
	root := c.MkDir()

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	// MockBootloader does not implement trusted assets
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"nested/other-asset",
		"shim",
	}

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(root, "asset"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(root, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	// mock some files in cache
	for _, name := range []string{
		fmt.Sprintf("asset-%s", dataHash),
		fmt.Sprintf("shim-%s", shimHash),
		"shim-newshimhash",
		"asset-newhash",
		"other-asset-newotherhash",
	} {
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			// new version added during update
			"asset": {dataHash, "newhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			// no new version added during update
			"asset": {dataHash},
			// new version added during update
			"shim": {shimHash, "newshimhash"},
			// completely new file
			"other-asset": {"newotherhash"},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "", "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "", "shim")
	c.Assert(err, IsNil)
	// the list of trusted assets was asked once for the boot bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// observe the recovery struct
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "shim")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "asset")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "nested/other-asset")
	c.Assert(err, IsNil)
	// and once again for the recovery bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// all files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
}

func (s *assetsSuite) TestUpdateObserverRollbackFileSanity(c *C) {
	root := c.MkDir()

	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	// MockBootloader does not implement trusted assets
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// sane state of modeenv before rollback
	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			// only one hash is listed, indicating it's a new file
			"asset": {"newhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			// same thing
			"asset": {"newhash"},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)
	// file does not exist on disk
	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "", "asset")
	c.Assert(err, IsNil)
	// the list of trusted assets was asked once for the boot bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// observe the recovery struct
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "asset")
	c.Assert(err, IsNil)
	// and once again for the recovery bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(newM.CurrentTrustedRecoveryBootAssets, HasLen, 0)

	// new observer
	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)
	m = boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			// only one hash is listed, indicating it's a new file
			"asset": {"newhash", "bogushash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			// same thing
			"asset": {"newhash", "bogushash"},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)
	// again, file does not exist on disk, but we expected it to be there
	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "", "asset")
	c.Assert(err, ErrorMatches, `tracked asset "asset" is unexpectedly missing from disk`)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "asset")
	c.Assert(err, ErrorMatches, `tracked asset "asset" is unexpectedly missing from disk`)

	// create the file which will fail checksum check
	err = ioutil.WriteFile(filepath.Join(root, "asset"), nil, 0644)
	c.Assert(err, IsNil)
	// once more, the file exists on disk, but has unexpected checksum
	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "", "asset")
	c.Assert(err, ErrorMatches, `unexpected content of existing asset "asset"`)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "", "asset")
	c.Assert(err, ErrorMatches, `unexpected content of existing asset "asset"`)
}

func (s *assetsSuite) TestUpdateObserverUpdateRollbackGrub(c *C) {
	// exercise a full update/rollback cycle with grub

	gadgetDir := c.MkDir()
	bootDir := c.MkDir()
	seedDir := c.MkDir()

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	cache := boot.NewTrustedAssetsCache(dirs.SnapBootAssetsDir)

	for _, dir := range []struct {
		root              string
		fileWithContent   [][]string
		addContentToCache bool
	}{
		{
			// data of boot bootloader
			root: bootDir,
			// SHA3-384: 0d0c6522fcc813770f2bb9ca68ad3b4f0ccc6b4bfbd2e8497030079e6146f92177ad8f6f83d96ab61d7d42f5228a4389
			fileWithContent: [][]string{
				{"EFI/boot/grubx64.efi", "grub efi"},
			},
			addContentToCache: true,
		}, {
			// data of seed bootloader
			root: seedDir,
			fileWithContent: [][]string{
				// SHA3-384: 6c3e6fc78ade5aadc5f9f0603a127346cc174436eb5e0188e108a376c3ba4d8951c460a8f51674e797c06951f74cb10d
				{"EFI/boot/grubx64.efi", "recovery grub efi"},
				// SHA3-384: c0437507ac094a7e9c699725cc0a4726cd10799af9eb79bbeaa136c2773163c80432295c2a04d3aa2ddd535ce8f1a12b
				{"EFI/boot/bootx64.efi", "recovery shim efi"},
			},
			addContentToCache: true,
		}, {
			// gadget content
			root: gadgetDir,
			fileWithContent: [][]string{
				// SHA3-384: f9554844308e89b565c1cdbcbdb9b09b8210dd2f1a11cb3b361de0a59f780ae3d4bd6941729a60e0f8ce15b2edef605d
				{"grubx64.efi", "new grub efi"},
				// SHA3-384: cc0663cc7e6c7ada990261c3ff1d72da001dc02451558716422d3d2443b8789463363c9ff0cd1b853c6ced3e8e7dc39d
				{"bootx64.efi", "new recovery shim efi"},
			},
		},
		// just the markers
		{
			root: bootDir,
			fileWithContent: [][]string{
				{"EFI/ubuntu/grub.cfg", "grub marker"},
			},
		}, {
			root: seedDir,
			fileWithContent: [][]string{
				{"EFI/ubuntu/grub.cfg", "grub marker"},
			},
		},
	} {
		for _, f := range dir.fileWithContent {
			p := filepath.Join(dir.root, f[0])
			err := os.MkdirAll(filepath.Dir(p), 0755)
			c.Assert(err, IsNil)
			err = ioutil.WriteFile(p, []byte(f[1]), 0644)
			c.Assert(err, IsNil)
			if dir.addContentToCache {
				_, err = cache.Add(p, "grub", filepath.Base(p))
				c.Assert(err, IsNil)
			}
		}
	}
	cacheContentBefore := []string{
		// recovery shim
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "bootx64.efi-c0437507ac094a7e9c699725cc0a4726cd10799af9eb79bbeaa136c2773163c80432295c2a04d3aa2ddd535ce8f1a12b"),
		// boot bootloader
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "grubx64.efi-0d0c6522fcc813770f2bb9ca68ad3b4f0ccc6b4bfbd2e8497030079e6146f92177ad8f6f83d96ab61d7d42f5228a4389"),
		// recovery bootloader
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "grubx64.efi-6c3e6fc78ade5aadc5f9f0603a127346cc174436eb5e0188e108a376c3ba4d8951c460a8f51674e797c06951f74cb10d"),
	}
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), cacheContentBefore)
	// current files are tracked
	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"grubx64.efi": {"0d0c6522fcc813770f2bb9ca68ad3b4f0ccc6b4bfbd2e8497030079e6146f92177ad8f6f83d96ab61d7d42f5228a4389"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"grubx64.efi": {"6c3e6fc78ade5aadc5f9f0603a127346cc174436eb5e0188e108a376c3ba4d8951c460a8f51674e797c06951f74cb10d"},
			"bootx64.efi": {"c0437507ac094a7e9c699725cc0a4726cd10799af9eb79bbeaa136c2773163c80432295c2a04d3aa2ddd535ce8f1a12b"},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// updates first
	_, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, bootDir, filepath.Join(gadgetDir, "grubx64.efi"), "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, seedDir, filepath.Join(gadgetDir, "grubx64.efi"), "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, seedDir, filepath.Join(gadgetDir, "bootx64.efi"), "EFI/boot/bootx64.efi")
	c.Assert(err, IsNil)
	// verify cache contents
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), []string{
		// recovery shim
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "bootx64.efi-c0437507ac094a7e9c699725cc0a4726cd10799af9eb79bbeaa136c2773163c80432295c2a04d3aa2ddd535ce8f1a12b"),
		// new recovery shim
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "bootx64.efi-cc0663cc7e6c7ada990261c3ff1d72da001dc02451558716422d3d2443b8789463363c9ff0cd1b853c6ced3e8e7dc39d"),
		// boot bootloader
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "grubx64.efi-0d0c6522fcc813770f2bb9ca68ad3b4f0ccc6b4bfbd2e8497030079e6146f92177ad8f6f83d96ab61d7d42f5228a4389"),
		// recovery bootloader
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "grubx64.efi-6c3e6fc78ade5aadc5f9f0603a127346cc174436eb5e0188e108a376c3ba4d8951c460a8f51674e797c06951f74cb10d"),
		// new recovery and boot bootloader
		filepath.Join(dirs.SnapBootAssetsDir, "grub", "grubx64.efi-f9554844308e89b565c1cdbcbdb9b09b8210dd2f1a11cb3b361de0a59f780ae3d4bd6941729a60e0f8ce15b2edef605d"),
	})

	// and modeenv contents
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"grubx64.efi": {
			// old hash
			"0d0c6522fcc813770f2bb9ca68ad3b4f0ccc6b4bfbd2e8497030079e6146f92177ad8f6f83d96ab61d7d42f5228a4389",
			// update
			"f9554844308e89b565c1cdbcbdb9b09b8210dd2f1a11cb3b361de0a59f780ae3d4bd6941729a60e0f8ce15b2edef605d",
		},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"grubx64.efi": {
			// old hash
			"6c3e6fc78ade5aadc5f9f0603a127346cc174436eb5e0188e108a376c3ba4d8951c460a8f51674e797c06951f74cb10d",
			// update
			"f9554844308e89b565c1cdbcbdb9b09b8210dd2f1a11cb3b361de0a59f780ae3d4bd6941729a60e0f8ce15b2edef605d",
		},
		"bootx64.efi": {
			// old hash
			"c0437507ac094a7e9c699725cc0a4726cd10799af9eb79bbeaa136c2773163c80432295c2a04d3aa2ddd535ce8f1a12b",
			// update
			"cc0663cc7e6c7ada990261c3ff1d72da001dc02451558716422d3d2443b8789463363c9ff0cd1b853c6ced3e8e7dc39d",
		},
	})

	// hiya, update failed, pretend we do a rollback, files on disk are as
	// if they were restored

	_, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, bootDir, "", "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, seedDir, "", "EFI/boot/grubx64.efi")
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, seedDir, "", "EFI/boot/bootx64.efi")
	c.Assert(err, IsNil)

	// modeenv is back to the initial state
	afterRollbackM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterRollbackM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterRollbackM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// and cache is back to the same state as before
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), cacheContentBefore)
}

func (s *assetsSuite) TestCopyBootAssetsCacheHappy(c *C) {
	newRoot := c.MkDir()
	// does not fail when dir does not exist
	err := boot.CopyBootAssetsCacheToRoot(newRoot)
	c.Assert(err, IsNil)

	// temporarily overide umask
	oldUmask := syscall.Umask(0000)
	defer syscall.Umask(oldUmask)

	entries := []struct {
		name, content string
		mode          uint
	}{
		{"foo/bar", "1234", 0644},
		{"grub/grubx64.efi-1234", "grub content", 0622},
		{"top-level", "top level content", 0666},
		{"deeply/nested/content", "deeply nested content", 0611},
	}

	for _, entry := range entries {
		p := filepath.Join(dirs.SnapBootAssetsDir, entry.name)
		err = os.MkdirAll(filepath.Dir(p), 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(p, []byte(entry.content), os.FileMode(entry.mode))
		c.Assert(err, IsNil)
	}

	err = boot.CopyBootAssetsCacheToRoot(newRoot)
	c.Assert(err, IsNil)
	for _, entry := range entries {
		p := filepath.Join(dirs.SnapBootAssetsDirUnder(newRoot), entry.name)
		c.Check(p, testutil.FileEquals, entry.content)
		fi, err := os.Stat(p)
		c.Assert(err, IsNil)
		c.Check(fi.Mode().Perm(), Equals, os.FileMode(entry.mode),
			Commentf("unexpected mode of copied file %q: %v", entry.name, fi.Mode().Perm()))
	}
}

func (s *assetsSuite) TestCopyBootAssetsCacheUnhappy(c *C) {
	// non-file
	newRoot := c.MkDir()
	dirs.SnapBootAssetsDir = c.MkDir()
	p := filepath.Join(dirs.SnapBootAssetsDir, "fifo")
	syscall.Mkfifo(p, 0644)
	err := boot.CopyBootAssetsCacheToRoot(newRoot)
	c.Assert(err, ErrorMatches, `unsupported non-file entry "fifo" mode prw-.*`)

	// non-writable root
	newRoot = c.MkDir()
	nonWritableRoot := filepath.Join(newRoot, "non-writable")
	err = os.MkdirAll(nonWritableRoot, 0000)
	c.Assert(err, IsNil)
	dirs.SnapBootAssetsDir = c.MkDir()
	err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "file"), nil, 0644)
	c.Assert(err, IsNil)
	err = boot.CopyBootAssetsCacheToRoot(nonWritableRoot)
	c.Assert(err, ErrorMatches, `cannot create cache directory under new root: mkdir .*: permission denied`)

	// file cannot be read
	newRoot = c.MkDir()
	dirs.SnapBootAssetsDir = c.MkDir()
	err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "file"), nil, 0000)
	c.Assert(err, IsNil)
	err = boot.CopyBootAssetsCacheToRoot(newRoot)
	c.Assert(err, ErrorMatches, `cannot copy boot asset cache file "file": failed to copy all: .*`)

	// directory at destination cannot be recreated
	newRoot = c.MkDir()
	dirs.SnapBootAssetsDir = c.MkDir()
	// make a directory at destination non writable
	err = os.MkdirAll(dirs.SnapBootAssetsDirUnder(newRoot), 0755)
	c.Assert(err, IsNil)
	err = os.Chmod(dirs.SnapBootAssetsDirUnder(newRoot), 0000)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "dir"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "dir", "file"), nil, 0000)
	c.Assert(err, IsNil)
	err = boot.CopyBootAssetsCacheToRoot(newRoot)
	c.Assert(err, ErrorMatches, `cannot recreate cache directory "dir": .*: permission denied`)

}
