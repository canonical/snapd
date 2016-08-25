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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
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

func Test(t *testing.T) { TestingT(t) }

type imageSuite struct {
	root       string
	bootloader *boottest.MockBootloader

	stdout *bytes.Buffer

	downloadedSnaps map[string]string
	storeSnapInfo   map[string]*snap.Info

	storeSigning *assertstest.StoreStack
	brandSigning *assertstest.SigningDB

	model *asserts.Model
}

var _ = Suite(&imageSuite{})

func (s *imageSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.bootloader = boottest.NewMockBootloader("grub", c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.stdout = bytes.NewBuffer(nil)
	image.Stdout = s.stdout
	s.downloadedSnaps = make(map[string]string)
	s.storeSnapInfo = make(map[string]*snap.Info)

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)

	brandPrivKey, _ := assertstest.GenerateKey(752)
	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	s.storeSigning.Add(brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")
	s.storeSigning.Add(brandAccKey)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"class":        "my-class",
		"architecture": "amd64",
		"store":        "canonical",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"core":         "core",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.model = model.(*asserts.Model)
}

func (s *imageSuite) TearDownTest(c *C) {
	partition.ForceBootloader(nil)
	image.Stdout = os.Stdout
}

// interface for the store
func (s *imageSuite) Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error) {
	return s.storeSnapInfo[name], nil
}

func (s *imageSuite) Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error) {
	return s.downloadedSnaps[name], nil
}

func (s *imageSuite) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	ref := &asserts.Ref{Type: assertType, PrimaryKey: primaryKey}
	return ref.Resolve(s.storeSigning.Find)
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
	_, err := image.DecodeModelAssertion(&image.Options{})
	c.Assert(err, ErrorMatches, "cannot read model assertion: open : no such file or directory")
}

func (s *imageSuite) TestIncorrectModelAssertions(c *C) {
	fn := filepath.Join(c.MkDir(), "broken-model.assertion")
	err := ioutil.WriteFile(fn, nil, 0644)
	c.Assert(err, IsNil)
	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot decode model assertion "%s": assertion content/signature separator not found`, fn))
}

func (s *imageSuite) TestValidButDifferentAssertion(c *C) {
	var differentAssertion = []byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-id: snap-id-1
snap-name: first
publisher-id: dev-id1
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`)

	fn := filepath.Join(c.MkDir(), "different.assertion")
	err := ioutil.WriteFile(fn, differentAssertion, 0644)
	c.Assert(err, IsNil)

	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`assertion in "%s" is not a model assertion`, fn))
}

func (s *imageSuite) TestHappyDecodeModelAssertion(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	a, err := image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.ModelType)
}

func (s *imageSuite) TestMissingGadgetUnpackDir(c *C) {
	err := image.DownloadUnpackGadget(s, s.model, &image.Options{})
	c.Assert(err, ErrorMatches, `cannot create gadget unpack dir "": mkdir : no such file or directory`)
}

func infoFromSnapYaml(c *C, snapYaml string, rev snap.Revision) *snap.Info {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	info.Revision = rev
	return info
}

func (s *imageSuite) TestDownloadUnpackGadget(c *C) {
	files := [][]string{
		{"subdir/canary.txt", "I'm a canary"},
	}
	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, files)
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(99))

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	err := image.DownloadUnpackGadget(s, s.model, &image.Options{
		GadgetUnpackDir: gadgetUnpackDir,
	})
	c.Assert(err, IsNil)

	// verify the right data got unpacked
	for _, t := range []struct{ file, content string }{
		{"meta/snap.yaml", packageGadget},
		{files[0][0], files[0][1]},
	} {
		fn := filepath.Join(gadgetUnpackDir, t.file)
		content, err := ioutil.ReadFile(fn)
		c.Assert(err, IsNil)
		c.Check(content, DeepEquals, []byte(t.content))
	}
}

func (s *imageSuite) TestBootstrapToRootDir(c *C) {
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")

	// FIXME: bootstrapToRootDir needs an unpacked gadget yaml
	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget")
	err := os.MkdirAll(gadgetUnpackDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.conf"), nil, 0644)
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

	err = image.BootstrapToRootDir(s, s.model, &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	})
	c.Assert(err, IsNil)

	// check the files are in place
	for _, fn := range []string{"pc_1.snap", "pc-kernel_2.snap", "core_3.snap"} {
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	storeAccountKey := s.storeSigning.StoreAccountKey("")
	brandPubKey, err := s.brandSigning.PublicKey("")
	c.Assert(err, IsNil)

	// check the assertions are in place
	for _, fn := range []string{"model", brandPubKey.ID() + ".account-key", "my-brand.account", storeAccountKey.PublicKeyID() + ".account-key"} {
		p := filepath.Join(rootdir, "var/lib/snapd/seed/assertions", fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	b, err := ioutil.ReadFile(filepath.Join(rootdir, "var/lib/snapd/seed/assertions", "model"))
	c.Assert(err, IsNil)
	c.Check(b, DeepEquals, asserts.Encode(s.model))

	b, err = ioutil.ReadFile(filepath.Join(rootdir, "var/lib/snapd/seed/assertions", "my-brand.account"))
	c.Assert(err, IsNil)
	a, err := asserts.Decode(b)
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountType)
	c.Check(a.HeaderString("account-id"), Equals, "my-brand")

	// check the bootloader config
	cv, err := s.bootloader.GetBootVar("snap_kernel")
	c.Assert(err, IsNil)
	c.Check(cv, Equals, "pc-kernel_2.snap")
	cv, err = s.bootloader.GetBootVar("snap_core")
	c.Assert(err, IsNil)
	c.Check(cv, Equals, "core_3.snap")
}
