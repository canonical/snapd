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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type imageSuite struct {
	root       string
	bootloader *boottest.MockBootloader

	downloadedSnap  string
	storeSnapResult *snap.Info
	storeRestorer   func()
}

var _ = Suite(&imageSuite{})

func (s *imageSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.bootloader = boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.storeRestorer = image.ReplaceStore(func(storeID string) image.Store {
		return s
	})
}

func (s *imageSuite) TearDownTest(c *C) {
	partition.ForceBootloader(nil)
	s.storeRestorer()
}

// interface for the store
func (s *imageSuite) Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error) {
	return s.storeSnapResult, nil
}

func (s *imageSuite) Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error) {
	return s.downloadedSnap, nil
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
type: core
`

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

func (s *imageSuite) TestMissingModelAssertions(c *C) {
	err := image.DownloadUnpackGadget(&image.Options{})
	c.Assert(err, ErrorMatches, "cannot read model assertion: open : no such file or directory")
}

func (s *imageSuite) TestIncorrectModelAssertions(c *C) {
	fn := filepath.Join(c.MkDir(), "broken-model.assertion")
	err := ioutil.WriteFile(fn, nil, 0644)
	c.Assert(err, IsNil)
	err = image.DownloadUnpackGadget(&image.Options{
		ModelAssertionFn: fn,
	})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot decode model assertion "%s": assertion content/signature separator not found`, fn))
}

func (s *imageSuite) TestMissingGadgetUnpackDir(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, modelAssertion, 0644)
	c.Assert(err, IsNil)

	err = image.DownloadUnpackGadget(&image.Options{
		ModelAssertionFn: fn,
	})
	c.Assert(err, ErrorMatches, `cannot create gadget unpack dir "": mkdir : no such file or directory`)
}

func (s *imageSuite) TestDownloadUnpackGadget(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, modelAssertion, 0644)
	c.Assert(err, IsNil)

	info, err := snap.InfoFromSnapYaml([]byte(packageGadget))
	c.Assert(err, IsNil)

	files := [][]string{
		{"subdir/canary.txt", "I'm a canary"},
	}
	mockGadget := snaptest.MakeTestSnapWithFiles(c, packageGadget, files)
	s.downloadedSnap = mockGadget
	s.storeSnapResult = info

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	err = image.DownloadUnpackGadget(&image.Options{
		ModelAssertionFn: fn,
		GadgetUnpackDir:  gadgetUnpackDir,
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
