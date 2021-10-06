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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type assetsSuite struct {
	baseBootenvSuite
}

var _ = Suite(&assetsSuite{})

func (s *assetsSuite) SetUpTest(c *C) {
	s.baseBootenvSuite.SetUpTest(c)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755), IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755), IsNil)

	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error { return nil })
	s.AddCleanup(restore)
}

func checkContentGlob(c *C, glob string, expected []string) {
	l, err := filepath.Glob(glob)
	c.Assert(err, IsNil)
	c.Check(l, DeepEquals, expected)
}

func (s *assetsSuite) uc20UpdateObserverEncryptedSystemMockedBootloader(c *C) (*boot.TrustedAssetsUpdateObserver, *asserts.Model) {
	// checked by TrustedAssetsUpdateObserverForModel and
	// resealKeyToModeenv
	s.stampSealedKeys(c, dirs.GlobalRootDir)
	return s.uc20UpdateObserver(c, c.MkDir())
}

func (s *assetsSuite) uc20UpdateObserver(c *C, gadgetDir string) (*boot.TrustedAssetsUpdateObserver, *asserts.Model) {
	uc20Model := boottest.MakeMockUC20Model()
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(obs, NotNil)
	c.Assert(err, IsNil)
	return obs, uc20Model
}

func (s *assetsSuite) bootloaderWithTrustedAssets(trustedAssets []string) *bootloadertest.MockTrustedAssetsBootloader {
	tab := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(tab)
	tab.TrustedAssetsList = trustedAssets
	s.AddCleanup(func() { bootloader.Force(nil) })
	return tab
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

	if os.Geteuid() != 0 {
		err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foo"), 0644)
		c.Assert(err, IsNil)
		// cannot create bootloader subdirectory
		ta, err := cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
		c.Assert(err, ErrorMatches, "cannot create cache directory: mkdir .*/grub: permission denied")
		c.Check(ta, IsNil)
	}

	// fix it now
	err = os.Chmod(cacheDir, 0755)
	c.Assert(err, IsNil)

	_, err = cache.Add(filepath.Join(d, "no-file"), "grub", "grubx64.efi")
	c.Assert(err, ErrorMatches, "cannot open asset file: open .*/no-file: no such file or directory")

	if os.Geteuid() != 0 {
		blDir := filepath.Join(cacheDir, "grub")
		defer os.Chmod(blDir, 0755)
		err = os.Chmod(blDir, 0000)
		c.Assert(err, IsNil)

		_, err = cache.Add(filepath.Join(d, "foobar"), "grub", "grubx64.efi")
		c.Assert(err, ErrorMatches, `cannot create temporary cache file: open .*/grub/grubx64\.efi\.temp\.[a-zA-Z0-9]+~: permission denied`)
	}
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
	// bootloader in gadget cannot be identified
	uc20Model := boottest.MakeMockUC20Model()
	for _, encryption := range []bool{true, false} {
		obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, encryption)
		c.Assert(err, ErrorMatches, "cannot find bootloader: cannot determine bootloader")
		c.Assert(obs, IsNil)
	}

	// pretend grub is used
	c.Assert(ioutil.WriteFile(filepath.Join(d, "grub.conf"), nil, 0755), IsNil)

	for _, encryption := range []bool{true, false} {
		obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, encryption)
		c.Assert(err, IsNil)
		c.Assert(obs, NotNil)
	}

	// but nil for non UC20
	nonUC20Model := boottest.MakeMockModel()
	nonUC20obs, err := boot.TrustedAssetsInstallObserverForModel(nonUC20Model, d, false)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)

	// listing trusted assets fails
	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
	})
	tab.TrustedAssetsErr = fmt.Errorf("fail")
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, true)
	c.Assert(err, ErrorMatches, `cannot list "trusted" bootloader trusted assets: fail`)
	c.Assert(obs, IsNil)
	// failed when listing run bootloader assets
	c.Check(tab.TrustedAssetsCalls, Equals, 1)

	// force an error
	bootloader.ForceError(fmt.Errorf("fail bootloader"))
	obs, err = boot.TrustedAssetsInstallObserverForModel(uc20Model, d, true)
	c.Assert(err, ErrorMatches, `cannot find bootloader: fail bootloader`)
	c.Assert(obs, IsNil)
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
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	otherData := []byte("other foobar")
	err = ioutil.WriteFile(filepath.Join(d, "other-foobar"), otherData, 0644)
	c.Assert(err, IsNil)

	writeChange := &gadget.ContentChange{
		// file that contains the data of the installed file
		After: filepath.Join(d, "foobar"),
		// there is no original file in place
		Before: "",
	}
	// only grubx64.efi gets installed to system-boot
	res, err := obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"EFI/boot/grubx64.efi", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// Observe is called when populating content, but one can freely specify
	// overlapping content entries, so a same file may be observed more than
	// once
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"EFI/boot/grubx64.efi", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// try with one more file, which is not a trusted asset of a run mode, so it is ignored
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"EFI/boot/bootx64.efi", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// a managed boot asset is to be held
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"EFI/ubuntu/grub.cfg", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)

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
	otherWriteChange := &gadget.ContentChange{
		After: filepath.Join(d, "other-foobar"),
	}
	res, err = obs.Observe(gadget.ContentWrite, systemSeedStruct, boot.InitramfsUbuntuBootDir,
		"EFI/boot/grubx64.efi", otherWriteChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
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

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/other-asset",
	})

	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	// the list of trusted assets was asked for run and recovery bootloaders
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	writeChange := &gadget.ContentChange{
		// file that contains the data of the installed file
		After: filepath.Join(d, "foobar"),
		// there is no original file in place
		Before: "",
	}
	res, err := obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"asset", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe same asset again
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"asset", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// different one
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"nested/other-asset", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// a non trusted asset
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"non-trusted", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// a single file in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("other-asset-%s", dataHash)),
	})
	// let's see what the observer has tracked
	tracked := obs.CurrentTrustedBootAssetsMap()
	c.Check(tracked, DeepEquals, boot.BootAssetsMap{
		"asset":       []string{dataHash},
		"other-asset": []string{dataHash},
	})
}

func (s *assetsSuite) TestInstallObserverObserveSystemBootMockedNoEncryption(c *C) {
	d := c.MkDir()
	s.bootloaderWithTrustedAssets([]string{"asset"})
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := false
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(obs, IsNil)
}

func (s *assetsSuite) TestInstallObserverObserveSystemBootMockedUnencryptedWithManaged(c *C) {
	d := c.MkDir()
	tab := s.bootloaderWithTrustedAssets([]string{"asset"})
	tab.ManagedAssetsList = []string{"managed"}
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := false
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	c.Assert(ioutil.WriteFile(filepath.Join(d, "foobar"), nil, 0755), IsNil)
	writeChange := &gadget.ContentChange{
		// file that contains the data of the installed file
		After: filepath.Join(d, "foobar"),
		// there is no original file in place
		Before: "",
	}
	res, err := obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir,
		"managed", writeChange)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)
}

func (s *assetsSuite) TestInstallObserverNonTrustedBootloader(c *C) {
	// bootloader is not a trusted assets one, but we use encryption, one
	// may try setting encryption key on the observer

	d := c.MkDir()

	// MockBootloader does not implement trusted assets
	bootloader.Force(bootloadertest.Mock("mock", ""))
	defer bootloader.Force(nil)

	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	obs.ChosenEncryptionKeys(secboot.EncryptionKey{1, 2, 3, 4}, secboot.EncryptionKey{5, 6, 7, 8})
	c.Check(obs.CurrentDataEncryptionKey(), DeepEquals, secboot.EncryptionKey{1, 2, 3, 4})
	c.Check(obs.CurrentSaveEncryptionKey(), DeepEquals, secboot.EncryptionKey{5, 6, 7, 8})
}

func (s *assetsSuite) TestInstallObserverTrustedButNoAssets(c *C) {
	// bootloader has no trusted assets, but encryption is enabled, and one
	// may try setting a key on the observer

	d := c.MkDir()

	tab := bootloadertest.Mock("trusted-assets", "").WithTrustedAssets()
	bootloader.Force(tab)
	defer bootloader.Force(nil)

	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	obs.ChosenEncryptionKeys(secboot.EncryptionKey{1, 2, 3, 4}, secboot.EncryptionKey{5, 6, 7, 8})
	c.Check(obs.CurrentDataEncryptionKey(), DeepEquals, secboot.EncryptionKey{1, 2, 3, 4})
	c.Check(obs.CurrentSaveEncryptionKey(), DeepEquals, secboot.EncryptionKey{5, 6, 7, 8})
}

func (s *assetsSuite) TestInstallObserverTrustedReuseNameErr(c *C) {
	d := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/asset",
	})

	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	// the list of trusted assets was asked for run and recovery bootloaders
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	err = ioutil.WriteFile(filepath.Join(d, "foobar"), []byte("foobar"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d, "other"), []byte("other"), 0644)
	c.Assert(err, IsNil)
	res, err := obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// same asset name but different content
	res, err = obs.Observe(gadget.ContentWrite, mockRunBootStruct, boot.InitramfsUbuntuBootDir, "nested/asset",
		&gadget.ContentChange{After: filepath.Join(d, "other")})
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
	c.Check(res, Equals, gadget.ChangeAbort)
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryMocked(c *C) {
	d := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/other-asset",
		"shim",
	})

	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	// trusted assets for the run and recovery bootloaders were asked for
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

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
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("other-asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// the list of trusted assets for recovery was asked for
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// let's see what the observer has tracked
	tracked := obs.CurrentTrustedRecoveryBootAssetsMap()
	c.Check(tracked, DeepEquals, boot.BootAssetsMap{
		"asset":       []string{dataHash},
		"other-asset": []string{dataHash},
		"shim":        []string{shimHash},
	})
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryReuseNameErr(c *C) {
	d := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/asset",
	})
	// we get an observer for UC20
	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	// got the list of trusted assets for run and recovery bootloaders
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

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
	// got the list of trusted assets for recovery bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
}

func (s *assetsSuite) TestInstallObserverObserveExistingRecoveryButMissingErr(c *C) {
	d := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
	})

	uc20Model := boottest.MakeMockUC20Model()
	useEncryption := true
	obs, err := boot.TrustedAssetsInstallObserverForModel(uc20Model, d, useEncryption)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	// trusted asset is missing
	err = obs.ObserveExistingTrustedRecoveryAssets(d)
	c.Assert(err, ErrorMatches, "cannot open asset file: .*/asset: no such file or directory")
}

func (s *assetsSuite) TestUpdateObserverNew(c *C) {
	tab := s.bootloaderWithTrustedAssets(nil)

	uc20Model := boottest.MakeMockUC20Model()

	gadgetDir := c.MkDir()

	// no trusted or managed assets
	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Check(obs, IsNil)

	// no managed, some trusted assets, but we are not tracking them
	tab.TrustedAssetsList = []string{"asset"}
	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Check(obs, IsNil)

	// let's see some managed assets, but not trusted assets
	tab.ManagedAssetsList = []string{"managed"}
	tab.TrustedAssetsList = nil
	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(err, IsNil)
	c.Check(obs, NotNil)

	// no managed, some trusted which we need to track
	s.stampSealedKeys(c, dirs.GlobalRootDir)
	tab.ManagedAssetsList = nil
	tab.TrustedAssetsList = []string{"asset"}
	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)

	// but nil for non UC20
	nonUC20Model := boottest.MakeMockModel()
	nonUC20obs, err := boot.TrustedAssetsUpdateObserverForModel(nonUC20Model, gadgetDir)
	c.Assert(err, Equals, boot.ErrObserverNotApplicable)
	c.Assert(nonUC20obs, IsNil)
}

func (s *assetsSuite) TestUpdateObserverUpdateMockedWithReseal(c *C) {
	// observe an update where some of the assets exist and some are new,
	// followed by reseal

	d := c.MkDir()
	backups := c.MkDir()
	root := c.MkDir()

	// try to arrange the backups like the updater would do it
	before := []byte("before")
	beforeHash := "2df0976fd45ba2392dc7985cdfb7c2d096c1ea4917929dd7a0e9bffae90a443271e702663fc6a4189c1f4ab3ce7daee3"
	err := ioutil.WriteFile(filepath.Join(backups, "asset.backup"), before, 0644)
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

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {beforeHash},
			"shim":  {"shim-hash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {beforeHash},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/other-asset",
		"shim",
	})
	tab.ManagedAssetsList = []string{
		"managed-asset",
	}

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
	// the list of trusted assets is obtained upfront
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content would get backed up by the updater
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "nested/other-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// all files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", beforeHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("other-asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {beforeHash, dataHash},
		"shim":  {"shim-hash", shimHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset":       {beforeHash, dataHash},
		"shim":        {shimHash},
		"other-asset": {dataHash},
	})

	// verify that managed assets are to be preserved
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "managed-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)

	// everything is set up, trigger a reseal
	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	err = obs.BeforeWrite()
	c.Assert(err, IsNil)
	c.Check(resealCalls, Equals, 1)
}

func (s *assetsSuite) TestUpdateObserverUpdateExistingAssetMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"shim",
	})
	tab.ManagedAssetsList = []string{
		"managed-asset",
		"nested/managed-asset",
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
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	// observe the updates
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
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

	// verify that managed assets are to be preserved
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "managed-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "nested/managed-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)

	// everything is set up, trigger reseal
	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	// execute before-write action
	err = obs.BeforeWrite()
	c.Assert(err, IsNil)
	c.Check(resealCalls, Equals, 1)
}

func (s *assetsSuite) TestUpdateObserverUpdateNothingTrackedMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
	})

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
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	// observe the updates
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
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

	// reseal does nothing
	err = obs.BeforeWrite()
	c.Assert(err, IsNil)
	c.Check(tab.RecoveryBootChainCalls, HasLen, 0)
	c.Check(tab.BootChainKernelPath, HasLen, 0)
}

func (s *assetsSuite) TestUpdateObserverUpdateOtherRoleStructMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
	})

	// modeenv is not set up, but the observer should not care

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
	// and once again for the recovery bootloader
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

	// non system-boot or system-seed structure gets ignored
	mockVolumeStruct := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Role: gadget.SystemData,
		},
	}

	// observe the updates
	res, err := obs.Observe(gadget.ContentUpdate, mockVolumeStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
}

func (s *assetsSuite) TestUpdateObserverUpdateTrivialErr(c *C) {
	// test trivial error scenarios of the update observer

	s.stampSealedKeys(c, dirs.GlobalRootDir)

	d := c.MkDir()
	root := c.MkDir()
	gadgetDir := c.MkDir()

	uc20Model := boottest.MakeMockUC20Model()

	// first no bootloader
	bootloader.ForceError(fmt.Errorf("bootloader fail"))

	obs, err := boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(obs, IsNil)
	c.Assert(err, ErrorMatches, "cannot find bootloader: bootloader fail")

	bootloader.ForceError(nil)
	bl := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	bl.TrustedAssetsErr = fmt.Errorf("fail")
	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(obs, IsNil)
	c.Assert(err, ErrorMatches, `cannot list "trusted" bootloader trusted assets: fail`)
	// failed listing trusted assets
	c.Check(bl.TrustedAssetsCalls, Equals, 1)

	// grab a new bootloader mock
	bl = bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	bl.TrustedAssetsList = []string{"asset"}

	obs, err = boot.TrustedAssetsUpdateObserverForModel(uc20Model, gadgetDir)
	c.Assert(err, IsNil)
	c.Assert(obs, NotNil)
	c.Check(bl.TrustedAssetsCalls, Equals, 2)

	// no modeenv
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot load modeenv: .* no such file or directory`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot load modeenv: .* no such file or directory`)
	c.Check(res, Equals, gadget.ChangeAbort)

	m := boot.Modeenv{
		Mode: "run",
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	// no source file, hash will fail
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot open asset file: .*/foobar: no such file or directory`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{Before: filepath.Join(d, "before"), After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot open asset file: .*/before: no such file or directory`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot open asset file: .*/foobar: no such file or directory`)
	c.Check(res, Equals, gadget.ChangeAbort)
}

func (s *assetsSuite) TestUpdateObserverUpdateRepeatedAssetErr(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	bl := bootloadertest.Mock("trusted", "").WithTrustedAssets()
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	bl.TrustedAssetsList = []string{"asset"}

	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

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
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// and the source file
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), nil, 0644)
	c.Assert(err, IsNil)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, ErrorMatches, `cannot reuse asset name "asset"`)
	c.Check(res, Equals, gadget.ChangeAbort)
}

func (s *assetsSuite) TestUpdateObserverUpdateAfterSuccessfulBootMocked(c *C) {
	//observe an update in a scenario when a mid-gadget-update reboot
	//happened and we have successfully booted with new assets only, but the
	//update is incomplete and gets started again

	d := c.MkDir()
	backups := c.MkDir()
	root := c.MkDir()

	// try to arrange the backups like the updater would do it
	before := []byte("before")
	beforeHash := "2df0976fd45ba2392dc7985cdfb7c2d096c1ea4917929dd7a0e9bffae90a443271e702663fc6a4189c1f4ab3ce7daee3"
	err := ioutil.WriteFile(filepath.Join(backups, "asset.backup"), before, 0644)
	c.Assert(err, IsNil)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	// pretend we rebooted mid update and have successfully booted with the
	// new assets already, the old asset may have been dropped from the cache already
	cache := boot.NewTrustedAssetsCache(dirs.SnapBootAssetsDir)
	_, err = cache.Add(filepath.Join(d, "foobar"), "trusted", "asset")
	c.Assert(err, IsNil)
	// file is in the cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
	})
	// and similarly, only the new asset in modeenv
	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	s.bootloaderWithTrustedAssets([]string{
		"asset",
	})

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content would get backed up by the updater
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)

	// all files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", beforeHash)),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		// original asset is restored, listed first
		"asset": {beforeHash, dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		// same here
		"asset": {beforeHash, dataHash},
	})
}

func (s *assetsSuite) TestUpdateObserverRollbackModeenvManipulationMocked(c *C) {
	root := c.MkDir()
	rootSeed := c.MkDir()
	d := c.MkDir()
	backups := c.MkDir()

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"nested/other-asset",
		"shim",
	})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	// file exists in both run and seed bootloader rootdirs
	c.Assert(ioutil.WriteFile(filepath.Join(root, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(rootSeed, "asset"), data, 0644), IsNil)
	// and in the gadget
	c.Assert(ioutil.WriteFile(filepath.Join(d, "asset"), data, 0644), IsNil)
	// would be listed as Before
	c.Assert(ioutil.WriteFile(filepath.Join(backups, "asset.backup"), data, 0644), IsNil)

	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	// only exists in seed bootloader rootdir
	c.Assert(ioutil.WriteFile(filepath.Join(rootSeed, "shim"), shim, 0644), IsNil)
	// and in the gadget
	c.Assert(ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644), IsNil)
	// would be listed as Before
	c.Assert(ioutil.WriteFile(filepath.Join(backups, "shim.backup"), data, 0644), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	// mock some files in cache
	for _, name := range []string{
		fmt.Sprintf("asset-%s", dataHash),
		fmt.Sprintf("shim-%s", shimHash),
		"shim-newshimhash",
		"asset-newhash",
		"other-asset-newotherhash",
	} {
		err := ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
	// the list of trusted assets is obtained upfront
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

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
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	res, err := obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{
			After:  filepath.Join(d, "asset"),
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{
			After: filepath.Join(d, "shim"),
			// no before content, new file
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, rootSeed, "shim",
		&gadget.ContentChange{
			After:  filepath.Join(d, "shim"),
			Before: filepath.Join(backups, "shim.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, rootSeed, "asset",
		&gadget.ContentChange{
			After:  filepath.Join(d, "asset"),
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, rootSeed, "nested/other-asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "asset"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
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

	tab := s.bootloaderWithTrustedAssets([]string{"asset"})

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
	// list of trusted assets is obtained upfront
	c.Check(tab.TrustedAssetsCalls, Equals, 2)

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
	err := m.WriteTo("")
	c.Assert(err, IsNil)
	// file does not exist on disk
	res, err := obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(newM.CurrentTrustedRecoveryBootAssets, HasLen, 0)

	// new observer
	obs, _ = s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
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
	res, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, ErrorMatches, `tracked asset "asset" is unexpectedly missing from disk`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, ErrorMatches, `tracked asset "asset" is unexpectedly missing from disk`)
	c.Check(res, Equals, gadget.ChangeAbort)

	// create the file which will fail checksum check
	err = ioutil.WriteFile(filepath.Join(root, "asset"), nil, 0644)
	c.Assert(err, IsNil)
	// once more, the file exists on disk, but has unexpected checksum
	res, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, ErrorMatches, `unexpected content of existing asset "asset"`)
	c.Check(res, Equals, gadget.ChangeAbort)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, root, "asset",
		&gadget.ContentChange{})
	c.Assert(err, ErrorMatches, `unexpected content of existing asset "asset"`)
	c.Check(res, Equals, gadget.ChangeAbort)
}

func (s *assetsSuite) TestUpdateObserverUpdateRollbackGrub(c *C) {
	// exercise a full update/rollback cycle with grub

	gadgetDir := c.MkDir()
	bootDir := c.MkDir()
	seedDir := c.MkDir()

	// prepare a marker for grub bootloader
	c.Assert(ioutil.WriteFile(filepath.Join(gadgetDir, "grub.conf"), nil, 0644), IsNil)

	// we get an observer for UC20
	s.stampSealedKeys(c, dirs.GlobalRootDir)
	obs, _ := s.uc20UpdateObserver(c, gadgetDir)

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
				{"grub.conf", "grub from gadget"},
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
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// updates first
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, bootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(gadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, seedDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{After: filepath.Join(gadgetDir, "grubx64.efi")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, seedDir, "EFI/boot/bootx64.efi",
		&gadget.ContentChange{After: filepath.Join(gadgetDir, "bootx64.efi")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// grub.cfg on ubuntu-seed and ubuntu-boot is managed by snapd
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, seedDir, "EFI/ubuntu/grub.cfg",
		&gadget.ContentChange{After: filepath.Join(gadgetDir, "grub.conf")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, seedDir, "EFI/ubuntu/grub.cfg",
		&gadget.ContentChange{After: filepath.Join(gadgetDir, "grub.conf")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)

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

	res, err = obs.Observe(gadget.ContentRollback, mockRunBootStruct, bootDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, seedDir, "EFI/boot/grubx64.efi",
		&gadget.ContentChange{})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentRollback, mockSeedStruct, seedDir, "EFI/boot/bootx64.efi",
		&gadget.ContentChange{})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)

	// modeenv is back to the initial state
	afterRollbackM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterRollbackM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterRollbackM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// and cache is back to the same state as before
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "grub", "*"), cacheContentBefore)
}

func (s *assetsSuite) TestUpdateObserverCanceledSimpleAfterBackupMocked(c *C) {
	d := c.MkDir()
	root := c.MkDir()

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
			"shim":  {"shimhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryhash"},
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// mock some files in cache
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"shim-shimhash",
		"asset-assethash",
		"asset-recoveryhash",
	} {
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"assethash", dataHash},
		"shim":  {"shimhash", shimHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"recoveryhash", dataHash},
		"shim":  {shimHash},
	})
	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	// update is canceled
	err = obs.Canceled()
	c.Assert(err, IsNil)
	// modeenv is back to initial state
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// unused assets were dropped
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})

	c.Check(resealCalls, Equals, 1)
}

func (s *assetsSuite) TestUpdateObserverCanceledPartiallyUsedMocked(c *C) {
	// cancel an update where one of the assets is already used and canceling does not remove it from the cache

	d := c.MkDir()
	root := c.MkDir()

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	// mock some files in cache
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"shim-shimhash",
		"asset-assethash",
		fmt.Sprintf("shim-%s", shimHash),
	} {
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
			"shim":  {"shimhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"shim": {shimHash},
		},
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	// XXX: shim is not updated
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// files are in cache
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"assethash", dataHash},
		"shim":  {"shimhash", shimHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
	// update is canceled
	err = obs.Canceled()
	c.Assert(err, IsNil)
	// modeenv is back to initial state
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// unused assets were dropped
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})
}

func (s *assetsSuite) TestUpdateObserverCanceledNoActionsMocked(c *C) {
	// make sure that when no ContentUpdate actions were registered, or some
	// were registered for one bootloader, but not the other, is not
	// triggering unwanted behavior on cancel

	d := c.MkDir()
	root := c.MkDir()

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
			"shim":  {"shimhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryhash"},
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// mock the files in cache
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"shim-shimhash",
		"asset-assethash",
		"asset-recoveryhash",
	} {
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})
	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	// cancel the update
	err = obs.Canceled()
	c.Assert(err, IsNil)
	// modeenv is unchanged
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// unused assets were dropped
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})

	c.Check(resealCalls, Equals, 0)

	err = ioutil.WriteFile(filepath.Join(d, "shim"), []byte("shim"), 0644)
	c.Assert(err, IsNil)
	// observe only recovery bootloader update, no action for run bootloader
	res, err := obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// cancel again
	err = obs.Canceled()
	c.Assert(err, IsNil)
	afterCancelM, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})
}

func (s *assetsSuite) TestUpdateObserverCanceledEmptyModeenvAssets(c *C) {
	// cancel an update where the maps of trusted assets are nil/empty
	d := c.MkDir()
	root := c.MkDir()
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})
	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	// trigger loading modeenv and bootloader information
	err = ioutil.WriteFile(filepath.Join(d, "shim"), []byte("shim"), 0644)
	c.Assert(err, IsNil)
	// observe an update only for the recovery bootloader, the run bootloader trusted assets remain empty
	res, err := obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)

	// cancel the update
	err = obs.Canceled()
	c.Assert(err, IsNil)
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, HasLen, 0)

	// get a new observer, and observe an update for run bootloader asset only
	obs, _ = s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// cancel once more
	err = obs.Canceled()
	c.Assert(err, IsNil)
	afterCancelM, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, HasLen, 0)
}

func (s *assetsSuite) TestUpdateObserverCanceledAfterRollback(c *C) {
	// pretend there are changed assets with hashes that are not listed in
	// modeenv
	d := c.MkDir()
	root := c.MkDir()

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})
	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	// trigger loading modeenv and bootloader information
	err = ioutil.WriteFile(filepath.Join(d, "shim"), []byte("shim"), 0644)
	c.Assert(err, IsNil)
	res, err := obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)

	// procure the desired state by:
	// injecting a changed asset for run bootloader
	recoveryAsset := true
	obs.InjectChangedAsset("trusted", "asset", "changehash", !recoveryAsset)
	// and a changed asset for recovery bootloader
	obs.InjectChangedAsset("trusted", "asset", "changehash", recoveryAsset)
	// completely unknown
	obs.InjectChangedAsset("trusted", "unknown", "somehash", !recoveryAsset)

	// cancel the update
	err = obs.Canceled()
	c.Assert(err, IsNil)
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
}

func (s *assetsSuite) TestUpdateObserverCanceledUnhappyCacheStillProceeds(c *C) {
	// make sure that trying to remove the file from cache will not break
	// the cancellation

	if os.Geteuid() == 0 {
		c.Skip("the test cannot be executed by the root user")
	}

	logBuf, restore := logger.MockLogger()
	defer restore()

	d := c.MkDir()
	root := c.MkDir()

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryhash"},
		},
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// mock the files in cache
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"asset-assethash",
		"asset-recoveryhash",
	} {
		err = ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})
	// we get an observer for UC20
	obs, _ := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)
	res, err := obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// make sure that the cache directory state is as expected
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	// and the file is added to the assets map
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"assethash"},
		"shim":  {shimHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {"recoveryhash"},
		"shim":  {shimHash},
	})

	// make cache directory read only and thus cache.Remove() fail
	c.Assert(os.Chmod(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0444), IsNil)
	defer os.Chmod(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755)

	// cancel should not fail, even though files cannot be removed from cache
	err = obs.Canceled()
	c.Assert(err, IsNil)
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})
	c.Check(logBuf.String(), Matches, fmt.Sprintf(`.* cannot remove unused boot asset shim:%s: .* permission denied\n`, shimHash))
}

func (s *assetsSuite) TestObserveSuccessfulBootNoTrusted(c *C) {
	// call to observe successful boot without any trusted assets

	m := &boot.Modeenv{
		Mode: "run",
		// no trusted assets
	}
	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Check(drop, IsNil)
	c.Check(newM, DeepEquals, m)
}

func (s *assetsSuite) TestObserveSuccessfulBootNoAssetsOnDisk(c *C) {
	// call to observe successful boot, but assets do not exist on disk

	s.bootloaderWithTrustedAssets([]string{"asset"})

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
		},
	}

	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Check(drop, IsNil)
	// we booted without assets on disk nonetheless
	c.Check(newM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(newM.CurrentTrustedRecoveryBootAssets, HasLen, 0)
}

func (s *assetsSuite) TestObserveSuccessfulBootAfterUpdate(c *C) {
	// call to observe successful boot

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	// only asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash", dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryassethash", dataHash},
			"shim":  {"recoveryshimhash", shimHash},
		},
	}

	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Assert(newM, NotNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
	c.Check(drop, HasLen, 3)
	for i, en := range []struct {
		assetName, hash string
	}{
		{"asset", "assethash"},
		{"asset", "recoveryassethash"},
		{"shim", "recoveryshimhash"},
	} {
		c.Check(drop[i].Equals("trusted", en.assetName, en.hash), IsNil)
	}
}

func (s *assetsSuite) TestObserveSuccessfulBootWithUnexpected(c *C) {
	// call to observe successful boot, but the asset we booted with is unexpected

	s.bootloaderWithTrustedAssets([]string{"asset"})

	data := []byte("foobar")
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	unexpected := []byte("unexpected")
	unexpectedHash := "2c823b62c52e614e48faac7e8b1fbb8ff3aee4d06b6f7fe5bd7d64953162b6e9879ead4827fa19c8c9a514585ddac94c"

	// asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), unexpected, 0644), IsNil)
	// and for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), unexpected, 0644), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash", dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"recoveryassethash", dataHash},
		},
	}

	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`system booted with unexpected run mode bootloader asset "asset" hash %v`, unexpectedHash))
	c.Assert(newM, IsNil)
	c.Check(drop, HasLen, 0)

	// make the run bootloader asset an expected one, we should still fail
	// on the recovery bootloader asset
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)

	newM, drop, err = boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`system booted with unexpected recovery bootloader asset "asset" hash %v`, unexpectedHash))
	c.Assert(newM, IsNil)
	c.Check(drop, HasLen, 0)
}

func (s *assetsSuite) TestObserveSuccessfulBootSingleEntries(c *C) {
	// call to observe successful boot

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	// only asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
			"shim":  {shimHash},
		},
	}

	// nothing is changed
	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Assert(newM, NotNil)
	c.Check(newM, DeepEquals, m)
	c.Check(drop, HasLen, 0)
}

func (s *assetsSuite) TestObserveSuccessfulBootDropCandidateUsedByOtherBootloader(c *C) {
	// observe successful boot, an unused recovery asset of a recovery
	// bootloader is used by the ubuntu-boot bootloader, so it cannot be
	// dropped from cache

	s.bootloaderWithTrustedAssets([]string{"asset"})

	maybeDrop := []byte("maybe-drop")
	maybeDropHash := "08a99ce3af529ebbfb9a82df690007ac650635b165c3d1b416d471907fa3843270dce9cc001ea26f4afb4e0c5af05209"
	data := []byte("foobar")
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	// ubuntu-boot booted with maybe-drop asset
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), maybeDrop, 0644), IsNil)

	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {maybeDropHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {maybeDropHash, dataHash},
		},
	}

	// nothing is changed
	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Assert(newM, NotNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {maybeDropHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	// nothing get dropped, maybe-drop asset is still used by the
	// ubuntu-boot bootloader
	c.Check(drop, HasLen, 0)
}

func (s *assetsSuite) TestObserveSuccessfulBootParallelUpdate(c *C) {
	// call to observe successful boot

	s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	data := []byte("foobar")
	// SHA3-384
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"
	shim := []byte("shim")
	shimHash := "dac0063e831d4b2e7a330426720512fc50fa315042f0bb30f9d1db73e4898dcb89119cac41fdfa62137c8931a50f9d7b"

	// only asset for ubuntu-boot
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0644), IsNil)
	// shim and asset for ubuntu-seed
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0644), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "shim"), shim, 0644), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"oldhash", dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"oldhash", dataHash},
			"shim":  {shimHash},
		},
	}

	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Assert(newM, NotNil)
	c.Check(newM.CurrentTrustedBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
	})
	c.Check(newM.CurrentTrustedRecoveryBootAssets, DeepEquals, boot.BootAssetsMap{
		"asset": {dataHash},
		"shim":  {shimHash},
	})
	// asset was updated in parallel on both partition from the same
	// oldhash that should be dropped now
	c.Check(drop, HasLen, 1)
	c.Check(drop[0].Equals("trusted", "asset", "oldhash"), IsNil)
}

func (s *assetsSuite) TestObserveSuccessfulBootHashErr(c *C) {
	// call to observe successful boot

	if os.Geteuid() == 0 {
		c.Skip("the test cannot be executed by the root user")
	}

	s.bootloaderWithTrustedAssets([]string{"asset"})

	data := []byte("foobar")
	dataHash := "0fa8abfbdaf924ad307b74dd2ed183b9a4a398891a2f6bac8fd2db7041b77f068580f9c6c66f699b496c2da1cbcc7ed8"

	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuBootDir, "asset"), data, 0000), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "asset"), data, 0000), IsNil)

	m := &boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {dataHash},
		},
	}

	// nothing is changed
	_, _, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, ErrorMatches, "cannot calculate the digest of existing trusted asset: .*/asset: permission denied")
}

func (s *assetsSuite) TestObserveSuccessfulBootDifferentMode(c *C) {
	s.bootloaderWithTrustedAssets([]string{"asset"})

	m := &boot.Modeenv{
		Mode: "recover",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"hash-1", "hash-2"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"hash-3", "hash-4"},
		},
	}

	// if we were in run mode, this would error out because the assets don't
	// exist, but we are not in run mode
	newM, drop, err := boot.ObserveSuccessfulBootWithAssets(m)
	c.Assert(err, IsNil)
	c.Assert(newM, DeepEquals, m)
	c.Assert(drop, IsNil)
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

	if os.Geteuid() == 0 {
		// the rest of the test cannot be executed by root user
		return
	}

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

func (s *assetsSuite) TestUpdateObserverReseal(c *C) {
	// observe an update followed by reseal

	d := c.MkDir()
	backups := c.MkDir()
	root := c.MkDir()

	// try to arrange the backups like the updater would do it
	before := []byte("before")
	beforeHash := "2df0976fd45ba2392dc7985cdfb7c2d096c1ea4917929dd7a0e9bffae90a443271e702663fc6a4189c1f4ab3ce7daee3"
	err := ioutil.WriteFile(filepath.Join(backups, "asset.backup"), before, 0644)
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

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
		"shim",
	})

	// we get an observer for UC20
	obs, uc20model := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {beforeHash},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {beforeHash},
		},
		CurrentRecoverySystems: []string{"recovery-system-label"},
		CurrentKernels:         []string{"pc-kernel_500.snap"},

		Model:          uc20model.Model(),
		BrandID:        uc20model.BrandID(),
		Grade:          string(uc20model.Grade()),
		ModelSignKeyID: uc20model.SignKeyID(),
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content would get backed up by the updater
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{
			After: filepath.Join(d, "foobar"),
			// original content
			Before: filepath.Join(backups, "asset.backup"),
		})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", beforeHash)),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)),
	})

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return uc20model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	// everything is set up, trigger a reseal

	resealCalls := 0
	shimBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("shim-%s", shimHash)), bootloader.RoleRecovery)
	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", dataHash)), bootloader.RoleRecovery)
	beforeAssetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted", fmt.Sprintf("asset-%s", beforeHash)), bootloader.RoleRecovery)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)

	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}
	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		runKernelBf,
	}

	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++

		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, uc20model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		switch resealCalls {
		case 1:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf)),
					secboot.NewLoadChain(beforeAssetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(runKernelBf)),
					secboot.NewLoadChain(beforeAssetBf,
						secboot.NewLoadChain(runKernelBf))),
			})
		case 2:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf)),
					secboot.NewLoadChain(beforeAssetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKey (call # %d)", resealCalls)
		}
		return nil
	})
	defer restore()

	err = obs.BeforeWrite()
	c.Assert(err, IsNil)
	c.Check(resealCalls, Equals, 2)
}

func (s *assetsSuite) TestUpdateObserverCanceledReseal(c *C) {
	// check that Canceled calls reseal when there were changes to the
	// trusted boot assets
	d := c.MkDir()
	root := c.MkDir()

	// mock some files in cache
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapBootAssetsDir, "trusted"), 0755), IsNil)
	for _, name := range []string{
		"shim-shimhash",
		"asset-assethash",
		"asset-recoveryhash",
	} {
		err := ioutil.WriteFile(filepath.Join(dirs.SnapBootAssetsDir, "trusted", name), nil, 0644)
		c.Assert(err, IsNil)
	}

	tab := s.bootloaderWithTrustedAssets([]string{"asset", "shim"})

	// we get an observer for UC20
	obs, uc20model := s.uc20UpdateObserverEncryptedSystemMockedBootloader(c)

	m := boot.Modeenv{
		Mode: "run",
		CurrentTrustedBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
			"shim":  {"shimhash"},
		},
		CurrentTrustedRecoveryBootAssets: boot.BootAssetsMap{
			"asset": {"assethash"},
			"shim":  {"shimhash"},
		},
		CurrentRecoverySystems:    []string{"system"},
		CurrentKernels:            []string{"pc-kernel_1.snap"},
		CurrentKernelCommandLines: boot.BootCommandLines{"snapd_recovery_mode=run"},

		Model:          uc20model.Model(),
		BrandID:        uc20model.BrandID(),
		Grade:          string(uc20model.Grade()),
		ModelSignKeyID: uc20model.SignKeyID(),
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	data := []byte("foobar")
	err = ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)
	shim := []byte("shim")
	err = ioutil.WriteFile(filepath.Join(d, "shim"), shim, 0644)
	c.Assert(err, IsNil)

	// trigger a bunch of updates, so that we have things to cancel
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// observe the recovery struct
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "shim",
		&gadget.ContentChange{After: filepath.Join(d, "shim")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return uc20model, []*seed.Snap{mockKernelSeedSnap(snap.R(1)), mockGadgetSeedSnap(c, nil)}, nil
	})
	defer restore()

	shimBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted/shim-shimhash"), bootloader.RoleRecovery)
	assetBf := bootloader.NewBootFile("", filepath.Join(dirs.SnapBootAssetsDir, "trusted/asset-assethash"), bootloader.RoleRecovery)
	recoveryKernelBf := bootloader.NewBootFile("/var/lib/snapd/seed/snaps/pc-kernel_1.snap", "kernel.efi", bootloader.RoleRecovery)
	runKernelBf := bootloader.NewBootFile(filepath.Join(s.rootdir, "var/lib/snapd/snaps/pc-kernel_500.snap"), "kernel.efi", bootloader.RoleRunMode)
	tab.RecoveryBootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		recoveryKernelBf,
	}
	tab.BootChainList = []bootloader.BootFile{
		bootloader.NewBootFile("", "shim", bootloader.RoleRecovery),
		bootloader.NewBootFile("", "asset", bootloader.RoleRecovery),
		runKernelBf,
	}

	resealCalls := 0
	restore = boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		c.Assert(params.ModelParams, HasLen, 1)
		mp := params.ModelParams[0]
		c.Check(mp.Model.Model(), Equals, uc20model.Model())
		for _, ch := range mp.EFILoadChains {
			printChain(c, ch, "-")
		}
		switch resealCalls {
		case 1:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(runKernelBf))),
			})
		case 2:
			c.Check(mp.EFILoadChains, DeepEquals, []*secboot.LoadChain{
				secboot.NewLoadChain(shimBf,
					secboot.NewLoadChain(assetBf,
						secboot.NewLoadChain(recoveryKernelBf))),
			})
		default:
			c.Errorf("unexpected additional call to secboot.ResealKey (call # %d)", resealCalls)
		}
		return nil
	})
	defer restore()

	// update is canceled
	err = obs.Canceled()
	c.Assert(err, IsNil)
	// modeenv is back to initial state
	afterCancelM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(afterCancelM.CurrentTrustedBootAssets, DeepEquals, m.CurrentTrustedBootAssets)
	c.Check(afterCancelM.CurrentTrustedRecoveryBootAssets, DeepEquals, m.CurrentTrustedRecoveryBootAssets)
	// unused assets were dropped
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), []string{
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-assethash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "asset-recoveryhash"),
		filepath.Join(dirs.SnapBootAssetsDir, "trusted", "shim-shimhash"),
	})

	c.Check(resealCalls, Equals, 2)
}

func (s *assetsSuite) TestUpdateObserverUpdateMockedNonEncryption(c *C) {
	// observe an update on a system where encryption is not used

	d := c.MkDir()
	backups := c.MkDir()
	root := c.MkDir()

	// try to arrange the backups like the updater would do it
	data := []byte("foobar")
	err := ioutil.WriteFile(filepath.Join(d, "foobar"), data, 0644)
	c.Assert(err, IsNil)

	m := boot.Modeenv{
		Mode: "run",
	}
	err = m.WriteTo("")
	c.Assert(err, IsNil)

	tab := s.bootloaderWithTrustedAssets([]string{
		"asset",
	})
	tab.ManagedAssetsList = []string{
		"managed-asset",
	}

	// we get an observer for UC20, bootloader is mocked
	obs, _ := s.uc20UpdateObserver(c, c.MkDir())

	// asset is ignored, and the change is applied
	change := &gadget.ContentChange{
		After: filepath.Join(d, "foobar"),
		// original content would get backed up by the updater
		Before: filepath.Join(backups, "asset.backup"),
	}
	res, err := obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "asset", change)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "asset", change)
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeApply)
	// trusted assets were asked for when setting up bootloader context
	c.Check(tab.TrustedAssetsCalls, Equals, 2)
	// but nothing is really tracked
	checkContentGlob(c, filepath.Join(dirs.SnapBootAssetsDir, "trusted", "*"), nil)
	// check modeenv
	newM, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(newM.CurrentTrustedBootAssets, HasLen, 0)
	c.Check(newM.CurrentTrustedRecoveryBootAssets, HasLen, 0)

	// verify that managed assets are to be preserved
	res, err = obs.Observe(gadget.ContentUpdate, mockRunBootStruct, root, "managed-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)
	res, err = obs.Observe(gadget.ContentUpdate, mockSeedStruct, root, "managed-asset",
		&gadget.ContentChange{After: filepath.Join(d, "foobar")})
	c.Assert(err, IsNil)
	c.Check(res, Equals, gadget.ChangeIgnore)

	// make sure that no reseal is triggered
	resealCalls := 0
	restore := boot.MockSecbootResealKeys(func(params *secboot.ResealKeysParams) error {
		resealCalls++
		return nil
	})
	defer restore()

	err = obs.BeforeWrite()
	c.Assert(err, IsNil)
	c.Check(resealCalls, Equals, 0)

	err = obs.Canceled()
	c.Assert(err, IsNil)
	c.Check(resealCalls, Equals, 0)
}
