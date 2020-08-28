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
	// MockBootloader does not implement trusted assets
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

func (s *assetsSuite) TestInstallObserverTrustedReuseNameErr(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("trusted-assets", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	tab.TrustedAssetsList = []string{
		"asset",
		"nested/asset",
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d, "other"), []byte("other"), 0644)
	c.Assert(err, IsNil)
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "foobar"), "asset")
	c.Assert(err, IsNil)
	// same asset name but different content
	_, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		filepath.Join(d, "other"), "nested/asset")
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
	// the list of trusted assets was asked for just once
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
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

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryMocked(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("recovery-bootloader", "").WithTrustedAssets()
	// MockBootloader does not implement trusted assets
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"nested/other-asset",
		"shim",
	}

	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "asset"), data, 0644)
	c.Assert(err, IsNil)
	err = os.Mkdir(filepath.Join(d, "nested"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d, "nested/other-asset"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, IsNil)
	// a single file in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "recovery-bootloader", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "recovery-bootloader", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "recovery-bootloader", fmt.Sprintf("other-asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "recovery-bootloader", fmt.Sprintf("shim-%s", shimHash)),
	})
	// the list of trusted assets was asked for just once
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// let's see what the observer has tracked
	tracked := obs.CurrentTrustedRecoveryBootAssetsMap()
	c.Check(tracked, DeepEquals, boot.BootAssetsMap{
		"asset":       []string{dataHash},
		"other-asset": []string{dataHash},
		"shim":        []string{shimHash},
	})
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryNoAssets(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("recovery-bootloader", "").WithTrustedAssets()
	// MockBootloader does not implement trusted assets
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	// does not fail when the bootloader has no trusted assets
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, IsNil)
	// asked for the list of trusted assets
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
	// nothing was tracked
	tracked := obs.CurrentTrustedRecoveryBootAssetsMap()
	c.Check(tracked, IsNil)

	// force a non trusted bootloader
	bl := bootloadertest.Mock("non-trusted-bootloader", "")
	bootloader.Force(bl)
	// happy with non trusted bootloader too
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, IsNil)
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryReuseNameErr(c *C) {
	d := c.MkDir()

	tab := bootloadertest.Mock("recovery-bootloader", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)
	tab.TrustedAssetsList = []string{
		"asset",
		"nested/asset",
	}
	// we get an observer for UC20
	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(d, "asset"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(d, "nested"), 0755)
	c.Assert(err, IsNil)
	// same asset name but different content
	err = ioutil.WriteFile(filepath.Join(d, "nested/asset"), []byte("other"), 0644)
	c.Assert(err, IsNil)
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	// same asset name but different content
	c.Assert(err, ErrorMatches, `cannot reuse recovery asset name "asset"`)
	// the list of trusted assets was asked for just once
	c.Check(tab.TrustedAssetsCalls, Equals, 1)
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryErr(c *C) {
	d := c.MkDir()

	uc20Model := makeMockUC20Model()
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)

	tab := bootloadertest.Mock("recovery-bootloader", "").WithTrustedAssets()
	// MockBootloader does not implement trusted assets
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	tab.TrustedAssetsList = []string{
		"asset",
	}

	// no trusted asset
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, ErrorMatches, "cannot open asset file: .*/asset: no such file or directory")
	c.Check(tab.TrustedAssetsCalls, Equals, 1)

	tab.TrustedAssetsErr = fmt.Errorf("fail")
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, ErrorMatches, `cannot list "recovery-bootloader" recovery bootloader trusted assets: fail`)
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	// force an error
	bootloader.ForceError(fmt.Errorf("fail bootloader"))
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, ErrorMatches, `cannot identify recovery system bootloader: fail bootloader`)
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
