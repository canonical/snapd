// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type emptyStore struct{}

func (s *emptyStore) SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, *auth.UserState, *store.RefreshOptions) ([]*snap.Info, error) {
	return nil, fmt.Errorf("cannot find snap")
}

func (s *emptyStore) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	return fmt.Errorf("cannot download")
}

func (s *emptyStore) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	return nil, &asserts.NotFoundError{Type: assertType}
}

func Test(t *testing.T) { TestingT(t) }

type imageSuite struct {
	testutil.BaseTest
	root       string
	bootloader *boottest.MockBootloader

	stdout *bytes.Buffer
	stderr *bytes.Buffer

	storeActions []*store.SnapAction
	tsto         *image.ToolingStore

	// SeedSnaps helps creating and making available seed snaps
	// (it provides MakeAssertedSnap etc.) for the tests.
	*seedtest.SeedSnaps

	model *asserts.Model
}

var _ = Suite(&imageSuite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *imageSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.bootloader = boottest.NewMockBootloader("grub", c.MkDir())
	bootloader.Force(s.bootloader)

	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.stdout = &bytes.Buffer{}
	image.Stdout = s.stdout
	s.stderr = &bytes.Buffer{}
	image.Stderr = s.stderr
	s.tsto = image.MockToolingStore(s)

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical", s)
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)
	s.DB = s.StoreSigning.Database

	s.model = s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my display name",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
	})

	otherAcct := assertstest.NewAccount(s.StoreSigning, "other", map[string]interface{}{
		"account-id": "other",
	}, "")
	s.StoreSigning.Add(otherAcct)

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	s.AddCleanup(c1.Restore)
	c2 := testutil.MockCommand(c, "umount", "")
	s.AddCleanup(c2.Restore)
}

func (s *imageSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	bootloader.Force(nil)
	image.Stdout = os.Stdout
	image.Stderr = os.Stderr
	s.storeActions = nil
}

// interface for the store
func (s *imageSuite) SnapAction(_ context.Context, _ []*store.CurrentSnap, actions []*store.SnapAction, _ *auth.UserState, _ *store.RefreshOptions) ([]*snap.Info, error) {
	if len(actions) != 1 {
		return nil, fmt.Errorf("expected 1 action, got %d", len(actions))
	}

	if actions[0].Action != "download" {
		return nil, fmt.Errorf("unexpected action %q", actions[0].Action)
	}

	if _, instanceKey := snap.SplitInstanceName(actions[0].InstanceName); instanceKey != "" {
		return nil, fmt.Errorf("unexpected instance key in %q", actions[0].InstanceName)
	}
	// record
	s.storeActions = append(s.storeActions, actions[0])

	if info := s.AssertedSnapInfo(actions[0].InstanceName); info != nil {
		info.Channel = actions[0].Channel
		return []*snap.Info{info}, nil
	}
	return nil, fmt.Errorf("no %q in the fake store", actions[0].InstanceName)
}

func (s *imageSuite) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	return osutil.CopyFile(s.AssertedSnap(name), targetFn, 0)
}

func (s *imageSuite) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	ref := &asserts.Ref{Type: assertType, PrimaryKey: primaryKey}
	return ref.Resolve(s.StoreSigning.Find)
}

const packageGadget = `
name: pc
version: 1.0
type: gadget
`

const packageGadgetWithBase = `
name: pc18
version: 1.0
type: gadget
base: core18
`
const packageClassicGadget = `
name: classic-gadget
version: 1.0
type: gadget
`

const packageClassicGadget18 = `
name: classic-gadget18
version: 1.0
type: gadget
base: core18
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

const packageCore18 = `
name: core18
version: 18.04
type: base
`

const snapdSnap = `
name: snapd
version: 3.14
type: snapd
`

const otherBase = `
name: other-base
version: 2.5029
type: base
`

const devmodeSnap = `
name: devmode-snap
version: 1.0
type: app
confinement: devmode
`

const classicSnap = `
name: classic-snap
version: 1.0
type: app
confinement: classic
`

const requiredSnap1 = `
name: required-snap1
version: 1.0
`

const requiredSnap18 = `
name: required-snap18
version: 1.0
base: core18
`

const snapReqOtherBase = `
name: snap-req-other-base
version: 1.0
base: other-base
`

const snapReqCore16Base = `
name: snap-req-core16-base
version: 1.0
base: core16
`

const snapReqContentProvider = `
name: snap-req-content-provider
version: 1.0
plugs:
 gtk-3-themes:
  interface: content
  default-provider: gtk-common-themes
  target: $SNAP/data-dir/themes
`

const snapBaseNone = `
name: snap-base-none
version: 1.0
base: none
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

func (s *imageSuite) TestModelAssertionNoParallelInstancesOfSnaps(c *C) {
	const mod = `type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget
kernel: kernel
required-snaps:
  - foo_instance
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, []byte(mod), 0644)
	c.Assert(err, IsNil)
	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Check(err, ErrorMatches, `.* assertion model: invalid snap name in "required-snaps" header: foo_instance`)
}

func (s *imageSuite) TestModelAssertionNoParallelInstancesOfKernel(c *C) {
	const mod = `type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget
kernel: kernel_instance
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, []byte(mod), 0644)
	c.Assert(err, IsNil)
	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Check(err, ErrorMatches, `.* assertion model: invalid snap name in "kernel" header: kernel_instance`)
}

func (s *imageSuite) TestModelAssertionNoParallelInstancesOfGadget(c *C) {
	const mod = `type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget_instance
kernel: kernel
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, []byte(mod), 0644)
	c.Assert(err, IsNil)
	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Check(err, ErrorMatches, `.* assertion model: invalid snap name in "gadget" header: brand-gadget_instance`)
}

func (s *imageSuite) TestModelAssertionNoParallelInstancesOfBase(c *C) {
	const mod = `type: model
authority-id: brand
series: 16
brand-id: brand
model: baz-3000
architecture: armhf
gadget: brand-gadget
kernel: kernel
base: core18_instance
timestamp: 2016-01-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, []byte(mod), 0644)
	c.Assert(err, IsNil)
	_, err = image.DecodeModelAssertion(&image.Options{
		ModelFile: fn,
	})
	c.Check(err, ErrorMatches, `.* assertion model: invalid snap name in "base" header: core18_instance`)
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
	err := image.DownloadUnpackGadget(s.tsto, s.model, &image.Options{}, nil)
	c.Assert(err, ErrorMatches, `cannot create gadget unpack dir "": mkdir : no such file or directory`)
}

func infoFromSnapYaml(c *C, snapYaml string, rev snap.Revision) *snap.Info {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	if !rev.Unset() {
		info.SnapID = info.InstanceName() + "-Id"
		info.Revision = rev
	}
	return info
}

func (s *imageSuite) TestDownloadUnpackGadget(c *C) {
	files := [][]string{
		{"subdir/canary.txt", "I'm a canary"},
	}
	s.MakeAssertedSnap(c, packageGadget, files, snap.R(99), "canonical")

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	opts := &image.Options{
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.DownloadUnpackGadget(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// verify the right data got unpacked
	for _, t := range []struct{ file, content string }{
		{"meta/snap.yaml", packageGadget},
		{files[0][0], files[0][1]},
	} {
		fn := filepath.Join(gadgetUnpackDir, t.file)
		c.Check(fn, testutil.FileEquals, t.content)
	}
}

func (s *imageSuite) TestDownloadUnpackGadgetFromTrack(c *C) {
	s.MakeAssertedSnap(c, packageGadget, nil, snap.R(1818), "canonical")

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{

		"architecture": "amd64",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
	})

	gadgetUnpackDir := filepath.Join(c.MkDir(), "gadget-unpack-dir")
	opts := &image.Options{
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.DownloadUnpackGadget(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	c.Check(s.storeActions, HasLen, 1)
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		Channel:      "18/stable",
		InstanceName: "pc",
	})

}

func (s *imageSuite) setupSnaps(c *C, gadgetUnpackDir string, publishers map[string]string) {
	if gadgetUnpackDir != "" {
		err := os.MkdirAll(gadgetUnpackDir, 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.conf"), nil, 0644)
		c.Assert(err, IsNil)
	}

	if _, ok := publishers["pc"]; ok {
		s.MakeAssertedSnap(c, packageGadget, [][]string{{"grub.cfg", "I'm a grub.cfg"}}, snap.R(1), publishers["pc"])
	}
	if _, ok := publishers["pc18"]; ok {
		s.MakeAssertedSnap(c, packageGadgetWithBase, [][]string{{"grub.cfg", "I'm a grub.cfg"}}, snap.R(4), publishers["pc18"])
	}

	if _, ok := publishers["classic-gadget"]; ok {
		s.MakeAssertedSnap(c, packageClassicGadget, [][]string{{"some-file", "Some file"}}, snap.R(5), publishers["classic-gadget"])
	}

	if _, ok := publishers["classic-gadget18"]; ok {
		s.MakeAssertedSnap(c, packageClassicGadget18, [][]string{{"some-file", "Some file"}}, snap.R(5), publishers["classic-gadget18"])
	}

	if _, ok := publishers["pc-kernel"]; ok {
		s.MakeAssertedSnap(c, packageKernel, nil, snap.R(2), publishers["pc-kernel"])
	}

	s.MakeAssertedSnap(c, packageCore, nil, snap.R(3), "canonical")

	s.MakeAssertedSnap(c, packageCore18, nil, snap.R(18), "canonical")
	s.MakeAssertedSnap(c, snapdSnap, nil, snap.R(18), "canonical")

	s.MakeAssertedSnap(c, otherBase, nil, snap.R(18), "other")

	s.MakeAssertedSnap(c, snapReqCore16Base, nil, snap.R(16), "other")

	s.MakeAssertedSnap(c, requiredSnap1, nil, snap.R(3), "other")
	s.AssertedSnapInfo("required-snap1").Contact = "foo@example.com"

	s.MakeAssertedSnap(c, requiredSnap18, nil, snap.R(6), "other")
	s.AssertedSnapInfo("required-snap18").Contact = "foo@example.com"

	s.MakeAssertedSnap(c, snapReqOtherBase, nil, snap.R(5), "other")

	s.MakeAssertedSnap(c, snapReqContentProvider, nil, snap.R(5), "other")

	s.MakeAssertedSnap(c, snapBaseNone, nil, snap.R(1), "other")
}

func (s *imageSuite) TestSetupSeed(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	seedassertsdir := filepath.Join(seeddir, "assertions")

	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(seeddir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core", "pc-kernel", "pc"} {
		info := s.AssertedSnapInfo(name)
		fn := filepath.Base(info.MountFile())
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:   name,
			SnapID: s.AssertedSnapID(name),
			File:   fn,
		})
		// sanity
		if name == "core" {
			c.Check(seedYaml.Snaps[i].SnapID, Equals, "coreidididididididididididididid")
		}
	}
	c.Check(seedYaml.Snaps[3].Name, Equals, "required-snap1")
	c.Check(seedYaml.Snaps[3].Contact, Equals, "foo@example.com")

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	brandPubKey := s.Brands.PublicKey("my-brand")

	// check the assertions are in place
	for _, fn := range []string{"model", brandPubKey.ID() + ".account-key", "my-brand.account", storeAccountKey.PublicKeyID() + ".account-key"} {
		p := filepath.Join(seedassertsdir, fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	c.Check(filepath.Join(seedassertsdir, "model"), testutil.FileEquals, asserts.Encode(s.model))
	b, err := ioutil.ReadFile(filepath.Join(seedassertsdir, "my-brand.account"))
	c.Assert(err, IsNil)
	a, err := asserts.Decode(b)
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountType)
	c.Check(a.HeaderString("account-id"), Equals, "my-brand")

	// check the snap assertions are also in place
	for _, snapName := range []string{"pc", "pc-kernel", "core"} {
		p := filepath.Join(seedassertsdir, fmt.Sprintf("16,%s.snap-declaration", s.AssertedSnapID(snapName)))
		c.Check(osutil.FileExists(p), Equals, true)
	}

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core", "snap_menuentry")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Check(m["snap_core"], Equals, "core_3.snap")
	c.Check(m["snap_menuentry"], Equals, "my display name")

	c.Check(s.stderr.String(), Equals, "")
}

func (s *imageSuite) TestSetupSeedLocalCoreBrandKernel(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	seedassertsdir := filepath.Join(rootdir, "var/lib/snapd/seed/assertions")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	})

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("core"),
			s.AssertedSnap("required-snap1"),
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	emptyToolingStore := image.MockToolingStore(&emptyStore{})
	local, err := image.LocalSnaps(emptyToolingStore, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core_x1.snap", "pc-kernel", "pc", "required-snap1_x1.snap"} {
		unasserted := false
		info := s.AssertedSnapInfo(name)
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

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	brandPubKey := s.Brands.PublicKey("my-brand")

	// check the assertions are in place
	for _, fn := range []string{"model", brandPubKey.ID() + ".account-key", "my-brand.account", storeAccountKey.PublicKeyID() + ".account-key"} {
		p := filepath.Join(seedassertsdir, fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	c.Check(filepath.Join(seedassertsdir, "model"), testutil.FileEquals, asserts.Encode(s.model))
	b, err := ioutil.ReadFile(filepath.Join(seedassertsdir, "my-brand.account"))
	c.Assert(err, IsNil)
	a, err := asserts.Decode(b)
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountType)
	c.Check(a.HeaderString("account-id"), Equals, "my-brand")

	decls, err := filepath.Glob(filepath.Join(seedassertsdir, "*.snap-declaration"))
	c.Assert(err, IsNil)
	// nothing for core
	c.Check(decls, HasLen, 2)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core_x1.snap")

	c.Check(s.stderr.String(), Equals, "WARNING: \"core\", \"required-snap1\" installed from local snaps disconnected from a store cannot be refreshed subsequently!\n")
}

func (s *imageSuite) TestSetupSeedDevmodeSnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	snapFile := snaptest.MakeTestSnapWithFiles(c, devmodeSnap, nil)

	opts := &image.Options{
		Snaps: []string{snapFile},

		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Channel:         "beta",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 5)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:    "core",
		SnapID:  s.AssertedSnapID("core"),
		File:    "core_3.snap",
		Channel: "beta",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:    "pc-kernel",
		SnapID:  s.AssertedSnapID("pc-kernel"),
		File:    "pc-kernel_2.snap",
		Channel: "beta",
	})
	c.Check(seedYaml.Snaps[2], DeepEquals, &seed.Snap{
		Name:    "pc",
		SnapID:  s.AssertedSnapID("pc"),
		File:    "pc_1.snap",
		Channel: "beta",
	})
	c.Check(seedYaml.Snaps[3], DeepEquals, &seed.Snap{
		Name:    "required-snap1",
		SnapID:  s.AssertedSnapID("required-snap1"),
		File:    "required-snap1_3.snap",
		Contact: "foo@example.com",
		Channel: "beta",
	})
	// ensure local snaps are put last in seed.yaml
	c.Check(seedYaml.Snaps[4], DeepEquals, &seed.Snap{
		Name:       "devmode-snap",
		DevMode:    true,
		Unasserted: true,
		File:       "devmode-snap_x1.snap",
		// no channel for unasserted snaps
		Channel: "",
	})

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
	last := len(seedYaml.Snaps) - 1
	c.Check(seedYaml.Snaps[last], DeepEquals, &seed.Snap{
		Name:       "devmode-snap",
		File:       fn,
		DevMode:    true,
		Unasserted: true,
	})
}

func (s *imageSuite) TestSetupSeedWithClassicSnapFails(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	s.MakeAssertedSnap(c, classicSnap, nil, snap.R(1), "other")

	opts := &image.Options{
		Snaps: []string{s.AssertedSnap("classic-snap")},

		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Channel:         "beta",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use classic snap "classic-snap" in a core system`)
}

func (s *imageSuite) TestSetupSeedWithBase(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"base":           "core18",
		"required-snaps": []interface{}{"other-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core18":     "canonical",
		"pc18":       "canonical",
		"pc-kernel":  "canonical",
		"snapd":      "canonical",
		"other-base": "other",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 5)

	// check the files are in place
	for i, name := range []string{"snapd", "pc-kernel", "core18_18.snap", "other-base", "pc18"} {
		unasserted := false
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   "core18ididididididididididididid",
						RealName: "core18",
						Revision: snap.R("18"),
					},
				}
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 5)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core18_18.snap")

	c.Check(s.stderr.String(), Equals, "")
}

func (s *imageSuite) TestSetupSeedWithBaseLegacySnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"base":           "core18",
		"required-snaps": []interface{}{"required-snap1"},
	})

	// required-snap1 needs core, for backward compatibility
	// we will add it implicitly but warn about this

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core18":    "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
		"snapd":     "canonical",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 6)

	// check the files are in place
	for i, name := range []string{"snapd", "core", "pc-kernel", "core18_18.snap", "pc18"} {
		unasserted := false
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   "core18ididididididididididididid",
						RealName: "core18",
						Revision: snap.R("18"),
					},
				}
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: unasserted,
		})
	}
	c.Check(seedYaml.Snaps[5].Name, Equals, "required-snap1")

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 6)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core18_18.snap")

	c.Check(s.stderr.String(), Equals, "WARNING: model has base \"core18\" but some snaps (\"required-snap1\") require \"core\" as base as well, for compatibility it was added implicitly, adding \"core\" explicitly is recommended\n")
}

func (s *imageSuite) TestSetupSeedKernelPublisherMismatch(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "other",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use kernel "pc-kernel" published by "other" for model by "my-brand"`)
}

func (s *imageSuite) TestInstallCloudConfigNoConfig(c *C) {
	targetDir := c.MkDir()
	emptyGadgetDir := c.MkDir()

	dirs.SetRootDir(targetDir)
	err := image.InstallCloudConfig(emptyGadgetDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(filepath.Join(targetDir, "etc/cloud")), Equals, false)
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
	c.Check(filepath.Join(targetDir, "etc/cloud/cloud.cfg"), testutil.FileEquals, canary)
}

func (s *imageSuite) TestNewToolingStoreWithAuth(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "auth.json")
	err := ioutil.WriteFile(authFn, []byte(`{
"macaroon": "MACAROON",
"discharges": ["DISCHARGE"]
}`), 0600)
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tsto, err := image.NewToolingStore()
	c.Assert(err, IsNil)
	user := tsto.User()
	c.Check(user.StoreMacaroon, Equals, "MACAROON")
	c.Check(user.StoreDischarges, DeepEquals, []string{"DISCHARGE"})
}

func (s *imageSuite) TestNewToolingStoreWithAuthFromSnapcraftLoginFile(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "auth.json")
	err := ioutil.WriteFile(authFn, []byte(`[login.ubuntu.com]
macaroon = MACAROON
unbound_discharge = DISCHARGE

`), 0600)
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tsto, err := image.NewToolingStore()
	c.Assert(err, IsNil)
	user := tsto.User()
	c.Check(user.StoreMacaroon, Equals, "MACAROON")
	c.Check(user.StoreDischarges, DeepEquals, []string{"DISCHARGE"})
}

func (s *imageSuite) TestSetupSeedLocalSnapsWithStoreAsserts(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	assertsdir := filepath.Join(rootdir, "var/lib/snapd/seed/assertions")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	})

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("core"),
			s.AssertedSnap("required-snap1"),
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc", "required-snap1_3.snap"} {
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "core",
						SnapID:   s.AssertedSnapID("core"),
						Revision: snap.R(3),
					},
				}
			case "required-snap1_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "required-snap1",
						SnapID:   s.AssertedSnapID("required-snap1"),
						Revision: snap.R(3),
					},
				}
			default:
				c.Errorf("cannot have %s", name)
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true, Commentf("cannot find %s", p))

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: false,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	brandPubKey := s.Brands.PublicKey("my-brand")

	// check the assertions are in place
	for _, fn := range []string{"model", brandPubKey.ID() + ".account-key", "my-brand.account", storeAccountKey.PublicKeyID() + ".account-key"} {
		p := filepath.Join(assertsdir, fn)
		c.Check(osutil.FileExists(p), Equals, true)
	}

	c.Check(filepath.Join(assertsdir, "model"), testutil.FileEquals, asserts.Encode(s.model))
	b, err := ioutil.ReadFile(filepath.Join(assertsdir, "my-brand.account"))
	c.Assert(err, IsNil)
	a, err := asserts.Decode(b)
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountType)
	c.Check(a.HeaderString("account-id"), Equals, "my-brand")

	decls, err := filepath.Glob(filepath.Join(assertsdir, "*.snap-declaration"))
	c.Assert(err, IsNil)
	c.Check(decls, HasLen, 4)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core_3.snap")

	c.Check(s.stderr.String(), Equals, "")
}

func (s *imageSuite) TestSetupSeedLocalSnapsWithChannels(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	})

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("required-snap1"),
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		SnapChannels: map[string]string{
			"core": "candidate",
			s.AssertedSnap("required-snap1"): "edge",
		},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc", "required-snap1_3.snap"} {
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "core",
						SnapID:   s.AssertedSnapID("core"),
						Revision: snap.R(3),
						Channel:  "candidate",
					},
				}
			case "required-snap1_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "required-snap1",
						SnapID:   s.AssertedSnapID("required-snap1"),
						Revision: snap.R(3),
						Channel:  "edge",
					},
				}
			default:
				c.Errorf("cannot have %s", name)
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true, Commentf("cannot find %s", p))

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			Channel:    info.Channel,
			File:       fn,
			Unasserted: false,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)
}

func (s *imageSuite) TestNoLocalParallelSnapInstances(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
		Snaps:     []string{"foo_instance"},
	})
	c.Assert(err, ErrorMatches, `cannot use snap "foo_instance", parallel snap instances are unsupported`)
}

func (s *imageSuite) TestNoInvalidSnapNames(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
		Snaps:     []string{"foo.invalid.name"},
	})
	c.Assert(err, ErrorMatches, `invalid snap name: "foo.invalid.name"`)
}

func (s *imageSuite) TestPrepareInvalidChannel(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
		Channel:   "x/x/x/x",
	})
	c.Assert(err, ErrorMatches, `cannot use channel: channel name has too many components: x/x/x/x`)
}

func (s *imageSuite) TestPrepareClassicModeNoClassicModel(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		Classic:   true,
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, "cannot prepare the image for a core model with --classic mode specified")
}

func (s *imageSuite) TestPrepareClassicModelNoClassicMode(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
	})

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, "--classic mode is required to prepare the image for a classic model")
}

func (s *imageSuite) TestPrepareClassicModelArchOverrideFails(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":      "true",
		"architecture": "amd64",
	})

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		Classic:      true,
		ModelFile:    fn,
		Architecture: "i386",
	})
	c.Assert(err, ErrorMatches, "cannot override model architecture: amd64")
}

func (s *imageSuite) TestPrepareClassicModelSnapsButNoArchFails(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
		"gadget":  "classic-gadget",
	})

	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		Classic:   true,
		ModelFile: fn,
	})
	c.Assert(err, ErrorMatches, "cannot have snaps for a classic image without an architecture in the model or from --arch")
}

func (s *imageSuite) TestSetupSeedWithKernelAndGadgetTrack(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:   "core",
		SnapID: s.AssertedSnapID("core"),
		File:   "core_3.snap",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:    "pc-kernel",
		SnapID:  s.AssertedSnapID("pc-kernel"),
		File:    "pc-kernel_2.snap",
		Channel: "18/stable",
	})
	c.Check(seedYaml.Snaps[2], DeepEquals, &seed.Snap{
		Name:    "pc",
		SnapID:  s.AssertedSnapID("pc"),
		File:    "pc_1.snap",
		Channel: "18/stable",
	})
}

func (s *imageSuite) TestSetupSeedWithKernelTrackWithDefaultChannel(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel=18",
	})

	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	rootdir := c.MkDir()
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Channel:         "edge",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:    "core",
		SnapID:  s.AssertedSnapID("core"),
		File:    "core_3.snap",
		Channel: "edge",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:    "pc-kernel",
		SnapID:  s.AssertedSnapID("pc-kernel"),
		File:    "pc-kernel_2.snap",
		Channel: "18/edge",
	})
	c.Check(seedYaml.Snaps[2], DeepEquals, &seed.Snap{
		Name:    "pc",
		SnapID:  s.AssertedSnapID("pc"),
		File:    "pc_1.snap",
		Channel: "edge",
	})
}

func (s *imageSuite) TestSetupSeedWithKernelTrackOnLocalSnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel=18",
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	// pretend we downloaded the core,kernel already
	cfn := s.AssertedSnap("core")
	kfn := s.AssertedSnap("pc-kernel")
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Snaps:           []string{kfn, cfn},
		Channel:         "beta",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	c.Check(local.NameToPath(), HasLen, 2)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:    "core",
		SnapID:  s.AssertedSnapID("core"),
		File:    "core_3.snap",
		Channel: "beta",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:    "pc-kernel",
		SnapID:  s.AssertedSnapID("pc-kernel"),
		File:    "pc-kernel_2.snap",
		Channel: "18/beta",
	})
}

func (s *imageSuite) TestSetupSeedWithBaseAndLocalLegacyCoreOrdering(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core18":    "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Snaps: []string{
			s.AssertedSnap("core"),
		},
	}
	emptyToolingStore := image.MockToolingStore(&emptyStore{})
	local, err := image.LocalSnaps(emptyToolingStore, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 6)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:   "snapd",
		SnapID: s.AssertedSnapID("snapd"),
		File:   "snapd_18.snap",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:       "core",
		Unasserted: true,
		File:       "core_x1.snap",
	})
	c.Check(seedYaml.Snaps[2], DeepEquals, &seed.Snap{
		Name:   "pc-kernel",
		SnapID: s.AssertedSnapID("pc-kernel"),
		File:   "pc-kernel_2.snap",
	})
	c.Check(seedYaml.Snaps[3], DeepEquals, &seed.Snap{
		Name:   "core18",
		SnapID: s.AssertedSnapID("core18"),
		File:   "core18_18.snap",
	})
	c.Check(seedYaml.Snaps[4], DeepEquals, &seed.Snap{
		Name:   "pc18",
		SnapID: s.AssertedSnapID("pc18"),
		File:   "pc18_4.snap",
	})
	c.Check(seedYaml.Snaps[5], DeepEquals, &seed.Snap{
		Name:    "required-snap1",
		SnapID:  s.AssertedSnapID("required-snap1"),
		File:    "required-snap1_3.snap",
		Contact: "foo@example.com",
	})
}

func (s *imageSuite) TestSetupSeedWithBaseAndLegacyCoreOrdering(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1", "core"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core18":    "canonical",
		"core":      "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	})

	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 6)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:   "snapd",
		SnapID: s.AssertedSnapID("snapd"),
		File:   "snapd_18.snap",
	})
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:   "core",
		SnapID: s.AssertedSnapID("core"),
		File:   "core_3.snap",
	})
	c.Check(seedYaml.Snaps[2], DeepEquals, &seed.Snap{
		Name:   "pc-kernel",
		SnapID: s.AssertedSnapID("pc-kernel"),
		File:   "pc-kernel_2.snap",
	})
	c.Check(seedYaml.Snaps[3], DeepEquals, &seed.Snap{
		Name:   "core18",
		SnapID: s.AssertedSnapID("core18"),
		File:   "core18_18.snap",
	})
	c.Check(seedYaml.Snaps[4], DeepEquals, &seed.Snap{
		Name:   "pc18",
		SnapID: s.AssertedSnapID("pc18"),
		File:   "pc18_4.snap",
	})
	c.Check(seedYaml.Snaps[5], DeepEquals, &seed.Snap{
		Name:    "required-snap1",
		SnapID:  s.AssertedSnapID("required-snap1"),
		File:    "required-snap1_3.snap",
		Contact: "foo@example.com",
	})
}

func (s *imageSuite) TestSetupSeedGadgetBaseModelBaseMismatch(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	// replace model with a model that uses core18 and a gadget
	// without a base
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core18":    "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use gadget snap because its base "" is different from model base "core18"`)
}

func (s *imageSuite) TestSetupSeedSnapReqBase(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-other-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, ErrorMatches, `cannot add snap "snap-req-other-base" without also adding its base "other-base" explicitly`)
}

func (s *imageSuite) TestSetupSeedBaseNone(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-base-none"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":           "canonical",
		"pc":             "canonical",
		"pc-kernel":      "canonical",
		"snap-base-none": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	c.Assert(image.SetupSeed(s.tsto, model, opts, local), IsNil)
}

func (s *imageSuite) TestSetupSeedSnapCoreSatisfiesCore16(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-core16-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TestSetupSeedStoreAssertionMissing(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"store":        "my-store",
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TestSetupSeedStoreAssertionFetched(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// add store assertion
	storeAs, err := s.StoreSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "my-store",
		"operator-id": "canonical",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.StoreSigning.Add(storeAs)
	c.Assert(err, IsNil)

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"store":        "my-store",
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedassertsdir := filepath.Join(seeddir, "assertions")

	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check the store assertion was fetched
	p := filepath.Join(seedassertsdir, "my-store.store")
	c.Check(osutil.FileExists(p), Equals, true)
}

func (s *imageSuite) TestSetupSeedSnapReqBaseFromLocal(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-other-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
		"other-base":          "canonical",
	})
	bfn := s.AssertedSnap("other-base")
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Snaps:           []string{bfn},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TestSetupSeedMissingContentProvider(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-content-provider"},
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":                  "canonical",
		"pc":                    "canonical",
		"pc-kernel":             "canonical",
		"snap-req-content-snap": "canonical",
	})
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Check(err, ErrorMatches, `cannot use snap "snap-req-content-provider" without its default content provider "gtk-common-themes" being added explicitly`)
}

func (s *imageSuite) TestMissingLocalSnaps(c *C) {
	opts := &image.Options{
		Snaps: []string{"i-am-missing.snap"},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, ErrorMatches, "local snap i-am-missing.snap not found")
	c.Assert(local, IsNil)
}

func (s *imageSuite) TestSetupSeedClassic(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// classic model with gadget etc
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget",
		"required-snaps": []interface{}{"required-snap1"},
	})

	rootdir := filepath.Join(c.MkDir(), "classic-image-root")
	s.setupSnaps(c, "", map[string]string{
		"classic-gadget": "my-brand",
	})

	opts := &image.Options{
		Classic: true,
		RootDir: rootdir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)

	// check the files are in place
	for i, name := range []string{"core", "classic-gadget", "required-snap1"} {
		unasserted := false
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Contact:    info.Contact,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 3)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})

	c.Check(s.stderr.String(), Matches, `WARNING: ensure that the contents under .*/var/lib/snapd/seed are owned by root:root in the \(final\) image`)

	// no blob dir created
	blobdir := filepath.Join(rootdir, "var/lib/snapd/snaps")
	c.Check(osutil.FileExists(blobdir), Equals, false)
}

func (s *imageSuite) TestSetupSeedClassicWithLocalClassicSnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// classic model
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":      "true",
		"architecture": "amd64",
	})

	rootdir := filepath.Join(c.MkDir(), "classic-image-root")
	s.setupSnaps(c, "", nil)

	snapFile := snaptest.MakeTestSnapWithFiles(c, classicSnap, nil)

	opts := &image.Options{
		Classic: true,
		Snaps:   []string{snapFile},
		RootDir: rootdir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 2)

	p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", "core_3.snap")
	c.Check(osutil.FileExists(p), Equals, true)
	c.Check(seedYaml.Snaps[0], DeepEquals, &seed.Snap{
		Name:   "core",
		SnapID: s.AssertedSnapID("core"),
		File:   "core_3.snap",
	})

	p = filepath.Join(rootdir, "var/lib/snapd/seed/snaps", "classic-snap_x1.snap")
	c.Check(osutil.FileExists(p), Equals, true)
	c.Check(seedYaml.Snaps[1], DeepEquals, &seed.Snap{
		Name:       "classic-snap",
		File:       "classic-snap_x1.snap",
		Classic:    true,
		Unasserted: true,
	})

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})
}

func (s *imageSuite) TestSetupSeedClassicSnapdOnly(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// classic model with gadget etc
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget18",
		"required-snaps": []interface{}{"core18", "required-snap18"},
	})

	rootdir := filepath.Join(c.MkDir(), "classic-image-root")
	s.setupSnaps(c, "", map[string]string{
		"classic-gadget18": "my-brand",
	})

	opts := &image.Options{
		Classic: true,
		RootDir: rootdir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"snapd", "core18", "classic-gadget18", "required-snap18"} {
		unasserted := false
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Contact:    info.Contact,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})

	c.Check(s.stderr.String(), Matches, `WARNING: ensure that the contents under .*/var/lib/snapd/seed are owned by root:root in the \(final\) image`)

	// no blob dir created
	blobdir := filepath.Join(rootdir, "var/lib/snapd/snaps")
	c.Check(osutil.FileExists(blobdir), Equals, false)
}

func (s *imageSuite) TestSetupSeedClassicNoSnaps(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// classic model with gadget etc
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
	})

	rootdir := filepath.Join(c.MkDir(), "classic-image-root")

	opts := &image.Options{
		Classic: true,
		RootDir: rootdir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seedYaml, err := seed.ReadYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 0)

	l, err := ioutil.ReadDir(filepath.Join(rootdir, "var/lib/snapd/seed/snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 0)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})

	// no blob dir created
	blobdir := filepath.Join(rootdir, "var/lib/snapd/snaps")
	c.Check(osutil.FileExists(blobdir), Equals, false)
}

func (s *imageSuite) TestSetupSeedClassicSnapdOnlyMissingCore16(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// classic model with gadget etc
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget18",
		"required-snaps": []interface{}{"core18", "snap-req-core16-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "classic-image-root")
	s.setupSnaps(c, "", map[string]string{
		"classic-gadget18": "my-brand",
	})

	opts := &image.Options{
		Classic: true,
		RootDir: rootdir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use "snap-req-core16-base" requiring base "core16" without adding "core16" \(or "core"\) explicitly`)
}

func (s *imageSuite) TestSnapChannel(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
	})

	opts := &image.Options{
		Channel: "stable",
		SnapChannels: map[string]string{
			"bar":       "beta",
			"pc-kernel": "edge",
		},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	ch, err := image.SnapChannel("foo", model, opts, local)
	c.Assert(err, IsNil)
	c.Check(ch, Equals, "stable")

	ch, err = image.SnapChannel("bar", model, opts, local)
	c.Assert(err, IsNil)
	c.Check(ch, Equals, "beta")

	ch, err = image.SnapChannel("pc", model, opts, local)
	c.Assert(err, IsNil)
	c.Check(ch, Equals, "18/stable")

	ch, err = image.SnapChannel("pc-kernel", model, opts, local)
	c.Assert(err, IsNil)
	c.Check(ch, Equals, "18/edge")

	opts.SnapChannels["bar"] = "lts/candidate"
	ch, err = image.SnapChannel("bar", model, opts, local)
	c.Assert(err, IsNil)
	c.Check(ch, Equals, "lts/candidate")

	opts.SnapChannels["pc-kernel"] = "lts/candidate"
	_, err = image.SnapChannel("pc-kernel", model, opts, local)
	c.Assert(err, ErrorMatches, `channel "lts/candidate" for kernel has a track incompatible with the track from model assertion: 18`)
}

func (s *imageSuite) TestSetupSeedLocalSnapd(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc18",
		"kernel":       "pc-kernel",
		"base":         "core18",
	})

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	})

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("snapd"),
			s.AssertedSnap("core18"),
		},

		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	emptyToolingStore := image.MockToolingStore(&emptyStore{})
	local, err := image.LocalSnaps(emptyToolingStore, opts)
	c.Assert(err, IsNil)

	err = image.SetupSeed(s.tsto, model, opts, local)
	c.Assert(err, IsNil)
	c.Assert(s.stdout.String(), Matches, `(?m)Copying ".*/snapd_3.14_all.snap" \(snapd\)`)
}

type toolingStoreContextSuite struct {
	sc store.DeviceAndAuthContext
}

var _ = Suite(&toolingStoreContextSuite{})

func (s *toolingStoreContextSuite) SetUpTest(c *C) {
	s.sc = image.ToolingStoreContext()
}

func (s *toolingStoreContextSuite) TestNopBits(c *C) {
	info, err := s.sc.CloudInfo()
	c.Assert(err, IsNil)
	c.Check(info, IsNil)

	device, err := s.sc.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	p, err := s.sc.DeviceSessionRequestParams("")
	c.Assert(err, Equals, store.ErrNoSerial)
	c.Check(p, IsNil)

	defURL, err := url.Parse("http://store")
	c.Assert(err, IsNil)
	proxyStoreID, proxyStoreURL, err := s.sc.ProxyStoreParams(defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, defURL)

	storeID, err := s.sc.StoreID("")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "")

	storeID, err = s.sc.StoreID("my-store")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-store")

	_, err = s.sc.UpdateDeviceAuth(nil, "")
	c.Assert(err, NotNil)
}

func (s *toolingStoreContextSuite) TestUpdateUserAuth(c *C) {
	u := &auth.UserState{
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge1"},
	}

	u1, err := s.sc.UpdateUserAuth(u, []string{"discharge2"})
	c.Assert(err, IsNil)
	c.Check(u1, Equals, u)
	c.Check(u1.StoreDischarges, DeepEquals, []string{"discharge2"})
}
