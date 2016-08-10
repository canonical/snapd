// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package image_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func makeFakeModelAssertion(c *C) string {
	var modelAssertion = []byte(`type: model
series: 16
authority-id: my-brand
brand-id: my-brand
model: my-model
class: my-class
allowed-modes:  
required-snaps:  
architecture: amd64
store: canonical
gadget: pc
kernel: pc-kernel
core: core
timestamp: 2016-01-02T10:00:00-05:00
body-length: 0

openpgpg 2cln
`)

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, modelAssertion, 0644)
	c.Assert(err, IsNil)
	return fn
}

type imageSuite struct {
	root       string
	bootloader *boottest.MockBootloader

	downloadedSnaps map[string]string
	storeSnapInfo   map[string]*snap.Info
	storeRestorer   func()
}

var _ = Suite(&imageSuite{})

func (s *imageSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.bootloader = boottest.NewMockBootloader("grub", c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.downloadedSnaps = make(map[string]string)
	s.storeSnapInfo = make(map[string]*snap.Info)
	s.storeRestorer = image.MockStoreNew(func(storeID string) image.Store {
		return s
	})
}

func (s *imageSuite) TearDownTest(c *C) {
	partition.ForceBootloader(nil)
	s.storeRestorer()
}

// interface for the store
func (s *imageSuite) Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error) {
	return s.storeSnapInfo[name], nil
}

func (s *imageSuite) Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error) {
	return s.downloadedSnaps[name], nil
}

const packageGadget = `
name: pc
version: 1.0
type: gadget
`

const packageKernel = `
name: pc-kernel
version: 4.4-1
type: kernel
`

const packageCore = `
name: core
version: 16.04
type: os
`

func (s *imageSuite) TestMissingModelAssertions(c *C) {
	err := image.DownloadUnpackGadget(&image.Options{})
	c.Assert(err, ErrorMatches, "cannot read model assertion: open : no such file or directory")
}

func (s *imageSuite) TestIncorrectModelAssertions(c *C) {
	fn := filepath.Join(c.MkDir(), "broken-model.assertion")
	err := ioutil.WriteFile(fn, nil, 0644)
	c.Assert(err, IsNil)
	err = image.DownloadUnpackGadget(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot decode model assertion "%s": assertion content/signature separator not found`, fn))
}

func (s *imageSuite) TestMissingGadgetUnpackDir(c *C) {
	fn := makeFakeModelAssertion(c)
	err := image.DownloadUnpackGadget(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, `cannot create gadget unpack dir "": mkdir : no such file or directory`)
}

func infoFromSnapYaml(c *C, snapYaml string, rev snap.Revision) *snap.Info {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	info.Revision = rev
	return info
}

func (s *imageSuite) TestDownloadUnpackGadget(c *C) {
	fn := makeFakeModelAssertion(c)
	files := [][]string{
		{"subdir/canary.txt", "I'm a canary"},
	}
	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, files)
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(99))

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	err := image.DownloadUnpackGadget(&image.Options{
		ModelFile:       fn,
		GadgetUnpackDir: gadgetUnpackDir,
	})
	c.Assert(err, IsNil)

	// verify the right data got unpacked
	for _, t := range []struct{ file, content string }{
		{"meta/snap.yaml", packageGadget},
		{files[0][0], files[0][1]},
	} {
		fn = filepath.Join(gadgetUnpackDir, t.file)
		content, err := ioutil.ReadFile(fn)
		c.Assert(err, IsNil)
		c.Check(content, DeepEquals, []byte(t.content))
	}
}

func (s *imageSuite) TestBootstrapToRootDir(c *C) {
	fn := makeFakeModelAssertion(c)
	rootdir := filepath.Join(c.MkDir(), "imageroot")

	// FIXME: bootstrapToRootDir needs an unpacked gadget yaml
	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget")
	err := os.MkdirAll(gadgetUnpackDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)

	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, [][]string{{"grub.cfg", "I'm a grub.cfg"}})
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(1))
	s.downloadedSnaps["pc-kernel"] = snaptest.MakeTestSnapWithFiles(c, packageKernel, nil)
	s.storeSnapInfo["pc-kernel"] = infoFromSnapYaml(c, packageKernel, snap.R(2))
	s.downloadedSnaps["core"] = snaptest.MakeTestSnapWithFiles(c, packageCore, nil)
	s.storeSnapInfo["core"] = infoFromSnapYaml(c, packageCore, snap.R(3))

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	defer c1.Restore()
	c2 := testutil.MockCommand(c, "umount", "")
	defer c2.Restore()

	err = image.BootstrapToRootDir(&image.Options{
		ModelFile:       fn,
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	})
	c.Assert(err, IsNil)

	// check the files are in place
	for _, fn := range []string{"pc_1.snap", "pc-kernel_2.snap", "core_3.snap"} {
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	// check the bootloader config
	cv, err := s.bootloader.GetBootVar("snap_kernel")
	c.Assert(err, IsNil)
	c.Check(cv, Equals, "pc-kernel_2.snap")
	cv, err = s.bootloader.GetBootVar("snap_core")
	c.Assert(err, IsNil)
	c.Check(cv, Equals, "core_3.snap")
}
