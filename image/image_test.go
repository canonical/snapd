// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
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

	downloadedSnaps map[string]string
	storeSnapInfo   map[string]*snap.Info
	storeActions    []*store.SnapAction
	tsto            *image.ToolingStore

	storeSigning *assertstest.StoreStack
	brandSigning *assertstest.SigningDB

	model *asserts.Model
}

var _ = Suite(&imageSuite{})

func (s *imageSuite) SetUpTest(c *C) {
	s.root = c.MkDir()
	s.bootloader = boottest.NewMockBootloader("grub", c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.stdout = &bytes.Buffer{}
	image.Stdout = s.stdout
	s.stderr = &bytes.Buffer{}
	image.Stderr = s.stderr
	s.downloadedSnaps = make(map[string]string)
	s.storeSnapInfo = make(map[string]*snap.Info)
	s.tsto = image.MockToolingStore(s)

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	brandPrivKey, _ := assertstest.GenerateKey(752)
	s.brandSigning = assertstest.NewSigningDB("my-brand", brandPrivKey)

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "verified",
	}, "")
	s.storeSigning.Add(brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, brandPrivKey.PublicKey(), "")
	s.storeSigning.Add(brandAccKey)

	model, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"display-name":   "my display name",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	s.model = model.(*asserts.Model)

	otherAcct := assertstest.NewAccount(s.storeSigning, "other", map[string]interface{}{
		"account-id": "other",
	}, "")
	s.storeSigning.Add(otherAcct)

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	s.AddCleanup(c1.Restore)
	c2 := testutil.MockCommand(c, "umount", "")
	s.AddCleanup(c2.Restore)
}

func (s *imageSuite) addSystemSnapAssertions(c *C, snapName string, publisher string) {
	snapID := snapName + "-Id"
	decl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"snap-name":    snapName,
		"publisher-id": publisher,
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
		"developer-id":  publisher,
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	partition.ForceBootloader(nil)
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

	if info, ok := s.storeSnapInfo[actions[0].InstanceName]; ok {
		info.Channel = actions[0].Channel
		return []*snap.Info{info}, nil
	}
	return nil, fmt.Errorf("no %q in the fake store", actions[0].InstanceName)
}

func (s *imageSuite) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
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

const packageGadgetWithBase = `
name: pc18
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
type: application
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

const requiredSnap1 = `
name: required-snap1
version: 1.0
`

const snapReqOtherBase = `
name: snap-req-other-base
version: 1.0
base: other-base
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
	c.Check(err, ErrorMatches, `cannot use snap "foo_instance", parallel snap instances are unsupported`)
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
	c.Check(err, ErrorMatches, `cannot use snap "kernel_instance", parallel snap instances are unsupported`)
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
	c.Check(err, ErrorMatches, `cannot use snap "brand-gadget_instance", parallel snap instances are unsupported`)
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
	c.Check(err, ErrorMatches, `cannot use snap "core18_instance", parallel snap instances are unsupported`)
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
	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, files)
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(99))

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
	s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, nil)
	s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(1818))

	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

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
	err := os.MkdirAll(gadgetUnpackDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetUnpackDir, "grub.conf"), nil, 0644)
	c.Assert(err, IsNil)

	if _, ok := publishers["pc"]; ok {
		s.downloadedSnaps["pc"] = snaptest.MakeTestSnapWithFiles(c, packageGadget, [][]string{{"grub.cfg", "I'm a grub.cfg"}})
		s.storeSnapInfo["pc"] = infoFromSnapYaml(c, packageGadget, snap.R(1))
		s.addSystemSnapAssertions(c, "pc", publishers["pc"])
	}
	if _, ok := publishers["pc18"]; ok {
		s.downloadedSnaps["pc18"] = snaptest.MakeTestSnapWithFiles(c, packageGadgetWithBase, [][]string{{"grub.cfg", "I'm a grub.cfg"}})
		s.storeSnapInfo["pc18"] = infoFromSnapYaml(c, packageGadgetWithBase, snap.R(4))
		s.addSystemSnapAssertions(c, "pc18", publishers["pc18"])
	}

	s.downloadedSnaps["pc-kernel"] = snaptest.MakeTestSnapWithFiles(c, packageKernel, nil)
	s.storeSnapInfo["pc-kernel"] = infoFromSnapYaml(c, packageKernel, snap.R(2))
	s.addSystemSnapAssertions(c, "pc-kernel", publishers["pc-kernel"])

	s.downloadedSnaps["core"] = snaptest.MakeTestSnapWithFiles(c, packageCore, nil)
	s.storeSnapInfo["core"] = infoFromSnapYaml(c, packageCore, snap.R(3))
	s.addSystemSnapAssertions(c, "core", "canonical")

	s.downloadedSnaps["core18"] = snaptest.MakeTestSnapWithFiles(c, packageCore18, nil)
	s.storeSnapInfo["core18"] = infoFromSnapYaml(c, packageCore18, snap.R(18))
	s.addSystemSnapAssertions(c, "core18", "canonical")

	s.downloadedSnaps["snapd"] = snaptest.MakeTestSnapWithFiles(c, snapdSnap, nil)
	s.storeSnapInfo["snapd"] = infoFromSnapYaml(c, snapdSnap, snap.R(18))
	s.addSystemSnapAssertions(c, "snapd", "canonical")

	s.downloadedSnaps["other-base"] = snaptest.MakeTestSnapWithFiles(c, otherBase, nil)
	s.storeSnapInfo["other-base"] = infoFromSnapYaml(c, otherBase, snap.R(18))
	s.addSystemSnapAssertions(c, "other-base", "other")

	s.downloadedSnaps["required-snap1"] = snaptest.MakeTestSnapWithFiles(c, requiredSnap1, nil)
	s.storeSnapInfo["required-snap1"] = infoFromSnapYaml(c, requiredSnap1, snap.R(3))
	s.storeSnapInfo["required-snap1"].Contact = "foo@example.com"
	s.addSystemSnapAssertions(c, "required-snap1", "other")

	s.downloadedSnaps["snap-req-other-base"] = snaptest.MakeTestSnapWithFiles(c, snapReqOtherBase, nil)
	s.storeSnapInfo["snap-req-other-base"] = infoFromSnapYaml(c, snapReqOtherBase, snap.R(5))
	s.addSystemSnapAssertions(c, "snap-req-other-base", "other")
}

func (s *imageSuite) TestBootstrapToRootDir(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
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

	err = image.BootstrapToRootDir(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(seeddir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core", "pc-kernel", "pc"} {
		info := s.storeSnapInfo[name]
		fn := filepath.Base(info.MountFile())
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seed.Snaps[i], DeepEquals, &snap.SeedSnap{
			Name:   name,
			SnapID: name + "-Id",
			File:   fn,
		})
	}
	c.Check(seed.Snaps[3].Name, Equals, "required-snap1")
	c.Check(seed.Snaps[3].Contact, Equals, "foo@example.com")

	storeAccountKey := s.storeSigning.StoreAccountKey("")
	brandPubKey, err := s.brandSigning.PublicKey("")
	c.Assert(err, IsNil)

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
	for _, snapId := range []string{"pc-Id", "pc-kernel-Id", "core-Id"} {
		p := filepath.Join(seedassertsdir, fmt.Sprintf("16,%s.snap-declaration", snapId))
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

func (s *imageSuite) TestBootstrapToRootDirLocalCoreBrandKernel(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
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
			s.downloadedSnaps["core"],
			s.downloadedSnaps["required-snap1"],
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	emptyToolingStore := image.MockToolingStore(&emptyStore{})
	local, err := image.LocalSnaps(emptyToolingStore, opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s.tsto, s.model, opts, local)
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
			Name:       info.InstanceName(),
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

	c.Check(s.stderr.String(), Equals, "WARNING: \"core\", \"required-snap1\" were installed from local snaps disconnected from a store and cannot be refreshed subsequently!\n")
}

func (s *imageSuite) TestBootstrapToRootDirDevmodeSnap(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	s.downloadedSnaps["devmode-snap"] = snaptest.MakeTestSnapWithFiles(c, devmodeSnap, nil)
	s.storeSnapInfo["devmode-snap"] = infoFromSnapYaml(c, devmodeSnap, snap.R(0))

	opts := &image.Options{
		Snaps: []string{s.downloadedSnaps["devmode-snap"]},

		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Channel:         "beta",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 5)
	c.Check(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		Name:    "core",
		SnapID:  "core-Id",
		File:    "core_3.snap",
		Channel: "beta",
	})
	c.Check(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		Name:    "pc-kernel",
		SnapID:  "pc-kernel-Id",
		File:    "pc-kernel_2.snap",
		Channel: "beta",
	})
	c.Check(seed.Snaps[2], DeepEquals, &snap.SeedSnap{
		Name:    "pc",
		SnapID:  "pc-Id",
		File:    "pc_1.snap",
		Channel: "beta",
	})
	c.Check(seed.Snaps[3], DeepEquals, &snap.SeedSnap{
		Name:    "required-snap1",
		SnapID:  "required-snap1-Id",
		File:    "required-snap1_3.snap",
		Contact: "foo@example.com",
		Channel: "beta",
	})
	// ensure local snaps are put last in seed.yaml
	c.Check(seed.Snaps[4], DeepEquals, &snap.SeedSnap{
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
	last := len(seed.Snaps) - 1
	c.Check(seed.Snaps[last], DeepEquals, &snap.SeedSnap{
		Name:       "devmode-snap",
		File:       fn,
		DevMode:    true,
		Unasserted: true,
	})
}

func (s *imageSuite) TestBootstrapWithBase(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"base":           "core18",
		"required-snaps": []interface{}{"other-base"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

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

	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 5)

	// check the files are in place
	for i, name := range []string{"snapd", "core18_18.snap", "pc-kernel", "pc18", "other-base"} {
		unasserted := false
		info := s.storeSnapInfo[name]
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   "core18-Id",
						RealName: "core18",
						Revision: snap.R("18"),
					},
				}
			}
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(rootdir, "var/lib/snapd/seed/snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		c.Check(seed.Snaps[i], DeepEquals, &snap.SeedSnap{
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

func (s *imageSuite) TestBootstrapToRootDirKernelPublisherMismatch(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
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

	err = image.BootstrapToRootDir(s.tsto, s.model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use kernel "pc-kernel" published by "other" for model by "my-brand"`)
}

func (s *imageSuite) TestInstallCloudConfigNoConfig(c *C) {
	targetDir := c.MkDir()
	emptyGadgetDir := c.MkDir()

	dirs.SetRootDir(targetDir)
	err := image.InstallCloudConfig(emptyGadgetDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(filepath.Join(targetDir, "etc/cloud")), Equals, true)
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

func (s *imageSuite) TestBootstrapToRootDirLocalSnapsWithStoreAsserts(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
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
			s.downloadedSnaps["core"],
			s.downloadedSnaps["required-snap1"],
		},
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s.tsto, s.model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc", "required-snap1_3.snap"} {
		info := s.storeSnapInfo[name]
		if info == nil {
			switch name {
			case "core_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "core",
						SnapID:   "core-Id",
						Revision: snap.R(3),
					},
				}
			case "required-snap1_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "required-snap1",
						SnapID:   "required-snap1-Id",
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

		c.Check(seed.Snaps[i], DeepEquals, &snap.SeedSnap{
			Name:       info.InstanceName(),
			SnapID:     info.SnapID,
			File:       fn,
			Unasserted: false,
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

func (s *imageSuite) TestBootstrapWithKernelAndGadgetTrack(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

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

	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 3)
	c.Check(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		Name:   "core",
		SnapID: "core-Id",
		File:   "core_3.snap",
	})
	c.Check(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		Name:    "pc-kernel",
		SnapID:  "pc-kernel-Id",
		File:    "pc-kernel_2.snap",
		Channel: "18/stable",
	})
	c.Check(seed.Snaps[2], DeepEquals, &snap.SeedSnap{
		Name:    "pc",
		SnapID:  "pc-Id",
		File:    "pc_1.snap",
		Channel: "18/stable",
	})
}

func (s *imageSuite) TestBootstrapWithKernelTrackWithDefaultChannel(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel=18",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

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

	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 3)
	c.Check(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		Name:    "core",
		SnapID:  "core-Id",
		File:    "core_3.snap",
		Channel: "edge",
	})
	c.Check(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		Name:    "pc-kernel",
		SnapID:  "pc-kernel-Id",
		File:    "pc-kernel_2.snap",
		Channel: "18/edge",
	})
	c.Check(seed.Snaps[2], DeepEquals, &snap.SeedSnap{
		Name:    "pc",
		SnapID:  "pc-Id",
		File:    "pc_1.snap",
		Channel: "edge",
	})
}

func (s *imageSuite) TestBootstrapWithKernelTrackOnLocalSnap(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel=18",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	})

	// pretend we downloaded the core,kernel already
	cfn := s.downloadedSnaps["core"]
	kfn := s.downloadedSnaps["pc-kernel"]
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Snaps:           []string{kfn, cfn},
		Channel:         "beta",
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	c.Check(local.NameToPath(), HasLen, 2)

	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 3)
	c.Check(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		Name:    "core",
		SnapID:  "core-Id",
		File:    "core_3.snap",
		Channel: "beta",
	})
	c.Check(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		Name:    "pc-kernel",
		SnapID:  "pc-kernel-Id",
		File:    "pc-kernel_2.snap",
		Channel: "18/beta",
	})
}

func (s *imageSuite) TestBootstrapWithBaseAndLegacyCoreOrdering(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)

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
		Snaps:           []string{"core"},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)

	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)

	// check seed yaml
	seed, err := snap.ReadSeedYaml(filepath.Join(rootdir, "var/lib/snapd/seed/seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seed.Snaps, HasLen, 6)
	c.Check(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		Name:   "snapd",
		SnapID: "snapd-Id",
		File:   "snapd_18.snap",
	})
	c.Check(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		Name:   "core",
		SnapID: "core-Id",
		File:   "core_3.snap",
	})
	c.Check(seed.Snaps[2], DeepEquals, &snap.SeedSnap{
		Name:   "core18",
		SnapID: "core18-Id",
		File:   "core18_18.snap",
	})
	c.Check(seed.Snaps[3], DeepEquals, &snap.SeedSnap{
		Name:   "pc-kernel",
		SnapID: "pc-kernel-Id",
		File:   "pc-kernel_2.snap",
	})
	c.Check(seed.Snaps[4], DeepEquals, &snap.SeedSnap{
		Name:   "pc18",
		SnapID: "pc18-Id",
		File:   "pc18_4.snap",
	})
	c.Check(seed.Snaps[5], DeepEquals, &snap.SeedSnap{
		Name:    "required-snap1",
		SnapID:  "required-snap1-Id",
		File:    "required-snap1_3.snap",
		Contact: "foo@example.com",
	})
}
func (s *imageSuite) TestBootstrapGadgetBaseModelBaseMismatch(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()
	// replace model with a model that uses core18 and a gadget
	// without a base
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required-snap1"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)
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
	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, ErrorMatches, `cannot use gadget snap because its base "" is different from model base "core18"`)
}

func (s *imageSuite) TestBootstrapToRootDirSnapReqBase(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-other-base"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)
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
	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, ErrorMatches, `cannot add snap "snap-req-other-base" without also adding its base "other-base" explicitly`)
}

func (s *imageSuite) TestBootstrapToRootDirSnapReqBaseFromLocal(c *C) {
	restore := image.MockTrusted(s.storeSigning.Trusted)
	defer restore()
	rawmodel, err := s.brandSigning.Sign(asserts.ModelType, map[string]interface{}{
		"series":         "16",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-other-base"},
		"timestamp":      time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := rawmodel.(*asserts.Model)
	rootdir := filepath.Join(c.MkDir(), "imageroot")
	gadgetUnpackDir := c.MkDir()
	s.setupSnaps(c, gadgetUnpackDir, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
		"other-base":          "canonical",
	})
	bfn := s.downloadedSnaps["other-base"]
	opts := &image.Options{
		RootDir:         rootdir,
		GadgetUnpackDir: gadgetUnpackDir,
		Snaps:           []string{bfn},
	}
	local, err := image.LocalSnaps(s.tsto, opts)
	c.Assert(err, IsNil)
	err = image.BootstrapToRootDir(s.tsto, model, opts, local)
	c.Assert(err, IsNil)
}

type toolingAuthContextSuite struct {
	ac auth.AuthContext
}

var _ = Suite(&toolingAuthContextSuite{})

func (s *toolingAuthContextSuite) SetUpTest(c *C) {
	s.ac = image.ToolingAuthContext()
}

func (s *toolingAuthContextSuite) TestNopBits(c *C) {
	info, err := s.ac.CloudInfo()
	c.Assert(err, IsNil)
	c.Check(info, IsNil)

	device, err := s.ac.Device()
	c.Assert(err, IsNil)
	c.Check(device, DeepEquals, &auth.DeviceState{})

	p, err := s.ac.DeviceSessionRequestParams("")
	c.Assert(err, Equals, auth.ErrNoSerial)
	c.Check(p, IsNil)

	defURL, err := url.Parse("http://store")
	c.Assert(err, IsNil)
	proxyStoreID, proxyStoreURL, err := s.ac.ProxyStoreParams(defURL)
	c.Assert(err, IsNil)
	c.Check(proxyStoreID, Equals, "")
	c.Check(proxyStoreURL, Equals, defURL)

	storeID, err := s.ac.StoreID("")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "")

	storeID, err = s.ac.StoreID("my-store")
	c.Assert(err, IsNil)
	c.Check(storeID, Equals, "my-store")

	_, err = s.ac.UpdateDeviceAuth(nil, "")
	c.Assert(err, NotNil)
}

func (s *toolingAuthContextSuite) TestUpdateUserAuth(c *C) {
	u := &auth.UserState{
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge1"},
	}

	u1, err := s.ac.UpdateUserAuth(u, []string{"discharge2"})
	c.Assert(err, IsNil)
	c.Check(u1, Equals, u)
	c.Check(u1.StoreDischarges, DeepEquals, []string{"discharge2"})
}
