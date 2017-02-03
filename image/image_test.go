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
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type imageSuite struct {
	root       string
	bootloader *boottest.MockBootloader

	stdout *bytes.Buffer
	stderr *bytes.Buffer

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

	s.stdout = &bytes.Buffer{}
	image.Stdout = s.stdout
	s.stderr = &bytes.Buffer{}
	image.Stderr = s.stderr
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
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.model = model.(*asserts.Model)
}

func (s *imageSuite) addSystemSnapAssertions(c *C, snapName string) {
	snapID := snapName + "-Id"
	decl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"snap-name":    snapName,
		"publisher-id": "can0nical",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(decl)
	c.Assert(err, IsNil)

	snapSHA3_384, snapSize, err := asserts.SnapFileSHA3_384(s.downloadedSnaps[snapName])
	c.Assert(err, IsNil)

	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": snapSHA3_384,
		"snap-size":     fmt.Sprintf("%d", snapSize),
		"snap-id":       snapID,
		"snap-revision": s.storeSnapInfo[snapName].Revision.String(),
		"developer-id":  "can0nical",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TearDownTest(c *C) {
	partition.ForceBootloader(nil)
	image.Stdout = os.Stdout
	image.Stderr = os.Stderr
}

// interface for the store
func (s *imageSuite) SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	return s.storeSnapInfo[spec.Name], nil
}

func (s *imageSuite) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) error {
	return osutil.CopyFile(s.downloadedSnaps[name], targetFn, 0)
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

const devmodeSnap = `
name: devmode-snap
version: 1.0
type: app
confinement: devmode
`

const requiredSnap1 = `
name: required-snap1
version: 1.0
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

func (s *imageSuite) TestModelAssertionReservedHeaders(c *C) {
	const mod = `type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget
kernel: kernel
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

	reserved := []string{
		"core",
		"os",
		"class",
		"allowed-modes",
	}

	for _, rsvd := range reserved {
		tweaked := strings.Replace(mod, "kernel: kernel\n", fmt.Sprintf("kernel: kernel\n%s: stuff\n", rsvd), 1)
		fn := filepath.Join(c.MkDir(), "model.assertion")
		err := ioutil.WriteFile(fn, []byte(tweaked), 0644)
		c.Assert(err, IsNil)
		_, err = image.DecodeModelAssertion(&image.Options{
			ModelFile: fn,
		})
		c.Check(err, ErrorMatches, fmt.Sprintf("model assertion cannot have reserved/unsupported header %q set", rsvd))
	}
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
	err := image.DownloadUnpackGadget(s, s.model, &image.Options{}, nil)
	c.Assert(err, ErrorMatches, `cannot create gadget unpack dir "": mkdir : no such file or directory`)
}

func infoFromSnapYaml(c *C, snapYaml string, rev snap.Revision) *snap.Info {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	if !rev.Unset() {
		info.SnapID = info.Name() + "-Id"
		info.Revision = rev
	}
	return info
}

func (s *imageSuite) TestDownloadUnpackGadget(c *C) {
	files := [][]string{
		{"subdir/canary.txt", "I'm a canary"},
	}
	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, files)
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(99))

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	opts := &image.Options{
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(opts)
	c.Assert(err, IsNil)

	err = image.DownloadUnpackGadget(s, s.model, opts, local)
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

func (s *imageSuite) setupSnaps(c *C, gadgetUnpackDir string) {
	err := os.MkdirAll(gadgetUnpackDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)

	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, [][]string{{"grub.cfg", "I'm a grub.cfg"}})
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(1))
	s.addSystemSnapAssertions(c, "pc")

	s.downloadedSnaps["pc-kernel"] = snaptest.MakeTestSnapWithFiles(c, packageKernel, nil)
	s.storeSnapInfo["pc-kernel"] = infoFromSnapYaml(c, packageKernel, snap.R(2))
	s.addSystemSnapAssertions(c, "pc-kernel")

	s.downloadedSnaps["core"] = snaptest.MakeTestSnapWithFiles(c, packageCore, nil)
	s.storeSnapInfo["core"] = infoFromSnapYaml(c, packageCore, snap.R(3))
	s.addSystemSnapAssertions(c, "core")

	s.downloadedSnaps["required-snap1"] = snaptest.MakeTestSnapWithFiles(c, requiredSnap1, nil)
	s.storeSnapInfo["required-snap1"] = infoFromSnapYaml(c, requiredSnap1, snap.R(3))
	s.addSystemSnapAssertions(c, "required-snap1")
}

func (s *imageSuite) TestBootstrapToRootDir(c *C) {
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")

	// FIXME: bootstrapToRootDir needs an unpacked gadget yaml
	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget")

	s.setupSnaps(c, gadgetUnpackDir)

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	defer c1.Restore()
	c2 := testutil.MockCommand(c, "umount", "")
	defer c2.Restore()

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core", "pc-kernel", "pc"} {
		info := s.storeSnapInfo[name]
		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seed.Snaps[i], DeepEquals, &snap.SeedSnap{
			Name:   name,
			SnapID: name + "-Id",
			File:   fn,
		})
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

	// check the snap assertions are also in place
	for _, snapId := range []string{"pc-Id", "pc-kernel-Id", "core-Id"} {
		p := filepath.Join(rootdir, "var/lib/snapd/seed/assertions", fmt.Sprintf("16,%s.snap-declaration", snapId))
		c.Check(osutil.FileExists(p), Equals, true)
	}

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Check(m["snap_core"], Equals, "core_3.snap")

	c.Check(s.stderr.String(), Equals, "")
}

func (s *imageSuite) TestBootstrapToRootDirLocalCore(c *C) {
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")

	// FIXME: bootstrapToRootDir needs an unpacked gadget yaml
	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget")

	s.setupSnaps(c, gadgetUnpackDir)

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	defer c1.Restore()
	c2 := testutil.MockCommand(c, "umount", "")
	defer c2.Restore()

	opts := &image.Options{
		Snaps: []string{
			s.downloadedSnaps["core"],
			s.downloadedSnaps["required-snap1"],
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core_x1.snap", "pc-kernel", "pc", "required-snap1_x1.snap"} {
		unasserted := false
		info := s.storeSnapInfo[name]
		if info == nil {
			switch name {
			case "core_x1.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "core",
						Revision: snap.R("x1"),
					},
				}
				unasserted = true
			case "required-snap1_x1.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "required-snap1",
						Revision: snap.R("x1"),
					},
				}
				unasserted = true
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seed.Snaps[i], DeepEquals, &snap.SeedSnap{
			Name:       info.Name(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

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

	decls, err := filepath.Glob(filepath.Join(rootdir, "var/lib/snapd/seed/assertions", "*.snap-declaration"))
	c.Assert(err, IsNil)
	// nothing for core
	c.Check(decls, HasLen, 2)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core_x1.snap")

	// check that cloud-init is setup correctly
	c.Check(osutil.FileExists(filepath.Join(rootdir, "etc/cloud/cloud-init.disabled")), Equals, true)

	c.Check(s.stderr.String(), Equals, `WARNING: "core", "required-snap1" were installed from local snaps disconnected from a store and cannot be refreshed subsequently!`)
}

func (s *imageSuite) TestBootstrapToRootDirDevmodeSnap(c *C) {
	restore := sysdb.InjectTrusted(s.storeSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")

	// FIXME: bootstrapToRootDir needs an unpacked gadget yaml
	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget")

	err := os.MkdirAll(gadgetUnpackDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)

	s.setupSnaps(c, gadgetUnpackDir)

	s.downloadedSnaps["devmode-snap"] = snaptest.MakeTestSnapWithFiles(c, devmodeSnap, nil)
	s.storeSnapInfo["devmode-snap"] = infoFromSnapYaml(c, devmodeSnap, snap.R(0))

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	defer c1.Restore()
	c2 := testutil.MockCommand(c, "umount", "")
	defer c2.Restore()

	opts := &image.Options{
		Snaps: []string{s.downloadedSnaps["devmode-snap"]},

		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 5)

	// check devmode-snap
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "devmode-snap",
			Revision: snap.R("x1"),
		},
	}
	fn := filepath.Base(info.MountFile())
	p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
	c.Check(osutil.FileExists(p), Equals, true)

	// ensure local snaps are put last in seed.yaml
	last := len(seed.Snaps) - 1
	c.Check(seed.Snaps[last], DeepEquals, &snap.SeedSnap{
		Name:       "devmode-snap",
		File:       fn,
		DevMode:    true,
		Unasserted: true,
	})
}

func (s *imageSuite) TestInstallCloudConfigNoConfig(c *C) {
	targetDir := c.MkDir()
	emptyGadgetDir := c.MkDir()

	dirs.SetRootDir(targetDir)
	err := image.InstallCloudConfig(emptyGadgetDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(filepath.Join(targetDir, "etc/cloud/cloud-init.disabled")), Equals, true)
}

func (s *imageSuite) TestInstallCloudConfigWithCloudConfig(c *C) {
	canary := []byte("ni! ni! ni!")

	targetDir := c.MkDir()
	gadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), canary, 0644)
	c.Assert(err, IsNil)

	dirs.SetRootDir(targetDir)
	err = image.InstallCloudConfig(gadgetDir)
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(targetDir, "etc/cloud/cloud.cfg"))
	c.Assert(err, IsNil)
	c.Check(content, DeepEquals, canary)
}
