// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/assets"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/tooling"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func Test(t *testing.T) { TestingT(t) }

type imageSuite struct {
	testutil.BaseTest
	root       string
	bootloader *bootloadertest.MockBootloader

	stdout *bytes.Buffer
	stderr *bytes.Buffer

	storeActionsBunchSizes []int
	storeActions           []*store.SnapAction
	curSnaps               [][]*store.CurrentSnap

	tsto *tooling.ToolingStore

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
	s.bootloader = bootloadertest.Mock("grub", c.MkDir())
	bootloader.Force(s.bootloader)

	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.stdout = &bytes.Buffer{}
	image.Stdout = s.stdout
	s.stderr = &bytes.Buffer{}
	image.Stderr = s.stderr
	s.tsto = tooling.MockToolingStore(s)

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)

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

	restore := image.MockWriteResolvedContent(func(_ string, _ *gadget.Info, _, _ string) error {
		return nil
	})
	s.AddCleanup(restore)
}

func (s *imageSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	bootloader.Force(nil)
	image.Stdout = os.Stdout
	image.Stderr = os.Stderr
	s.storeActions = nil
	s.storeActionsBunchSizes = nil
	s.curSnaps = nil
}

// interface for the store
func (s *imageSuite) SnapAction(_ context.Context, curSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, _ *auth.UserState, _ *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		return nil, nil, fmt.Errorf("unexpected assertion query")
	}

	s.storeActionsBunchSizes = append(s.storeActionsBunchSizes, len(actions))
	s.curSnaps = append(s.curSnaps, curSnaps)
	sars := make([]store.SnapActionResult, 0, len(actions))
	for _, a := range actions {
		if a.Action != "download" {
			return nil, nil, fmt.Errorf("unexpected action %q", a.Action)
		}

		if _, instanceKey := snap.SplitInstanceName(a.InstanceName); instanceKey != "" {
			return nil, nil, fmt.Errorf("unexpected instance key in %q", a.InstanceName)
		}
		// record
		s.storeActions = append(s.storeActions, a)

		info := s.AssertedSnapInfo(a.InstanceName)
		if info == nil {
			return nil, nil, fmt.Errorf("no %q in the fake store", a.InstanceName)
		}
		info1 := *info
		channel := a.Channel
		redirectChannel := ""
		if strings.HasPrefix(a.InstanceName, "default-track-") {
			channel = "default-track/stable"
			redirectChannel = channel
		}
		info1.Channel = channel
		sars = append(sars, store.SnapActionResult{
			Info:            &info1,
			RedirectChannel: redirectChannel,
		})
	}

	return sars, nil, nil
}

func (s *imageSuite) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	return osutil.CopyFile(s.AssertedSnap(name), targetFn, 0)
}

func (s *imageSuite) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	ref := &asserts.Ref{Type: assertType, PrimaryKey: primaryKey}
	return ref.Resolve(s.StoreSigning.Find)
}

// TODO: use seedtest.SampleSnapYaml for some of these
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

const defaultTrackSnap18 = `
name: default-track-snap18
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

func (s *imageSuite) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) {
	s.SeedSnaps.MakeAssertedSnap(c, snapYaml, files, revision, developerID, s.StoreSigning.Database)
}

const stableChannel = "stable"

const pcGadgetYaml = `
 volumes:
   pc:
     bootloader: grub
 `

const pcUC20GadgetYaml = `
 volumes:
   pc:
     bootloader: grub
     structure:
       - name: ubuntu-seed
         role: system-seed
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 100M
       - name: ubuntu-data
         role: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 200M
 `

const piUC20GadgetYaml = `
 volumes:
   pi:
     schema: mbr
     bootloader: u-boot
     structure:
       - name: ubuntu-seed
         role: system-seed
         type: 0C
         size: 100M
       - name: ubuntu-data
         role: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 200M
 `

func (s *imageSuite) setupSnaps(c *C, publishers map[string]string, defaultsYaml string) {
	gadgetYaml := pcGadgetYaml + defaultsYaml
	if _, ok := publishers["pc"]; ok {
		s.MakeAssertedSnap(c, packageGadget, [][]string{
			{"grub.conf", ""}, {"grub.cfg", "I'm a grub.cfg"},
			{"meta/gadget.yaml", gadgetYaml},
		}, snap.R(1), publishers["pc"])
	}
	if _, ok := publishers["pc18"]; ok {
		s.MakeAssertedSnap(c, packageGadgetWithBase, [][]string{
			{"grub.conf", ""}, {"grub.cfg", "I'm a grub.cfg"},
			{"meta/gadget.yaml", gadgetYaml},
		}, snap.R(4), publishers["pc18"])
	}

	if _, ok := publishers["classic-gadget"]; ok {
		s.MakeAssertedSnap(c, packageClassicGadget, [][]string{
			{"some-file", "Some file"},
		}, snap.R(5), publishers["classic-gadget"])
	}

	if _, ok := publishers["classic-gadget18"]; ok {
		s.MakeAssertedSnap(c, packageClassicGadget18, [][]string{
			{"some-file", "Some file"},
		}, snap.R(5), publishers["classic-gadget18"])
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
	s.AssertedSnapInfo("required-snap1").EditedContact = "mailto:foo@example.com"

	s.MakeAssertedSnap(c, requiredSnap18, nil, snap.R(6), "other")
	s.AssertedSnapInfo("required-snap18").EditedContact = "mailto:foo@example.com"

	s.MakeAssertedSnap(c, defaultTrackSnap18, nil, snap.R(5), "other")

	s.MakeAssertedSnap(c, snapReqOtherBase, nil, snap.R(5), "other")

	s.MakeAssertedSnap(c, snapReqContentProvider, nil, snap.R(5), "other")

	s.MakeAssertedSnap(c, snapBaseNone, nil, snap.R(1), "other")
}

func (s *imageSuite) loadSeed(c *C, seeddir string) (essSnaps []*seed.Snap, runSnaps []*seed.Snap, roDB asserts.RODatabase) {
	label := ""
	systems, err := filepath.Glob(filepath.Join(seeddir, "systems", "*"))
	c.Assert(err, IsNil)
	if len(systems) > 1 {
		c.Fatal("expected at most 1 Core 20 recovery system")
	} else if len(systems) == 1 {
		label = filepath.Base(systems[0])
	}

	sd, err := seed.Open(seeddir, label)
	c.Assert(err, IsNil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)

	commitTo := func(b *asserts.Batch) error {
		return b.CommitTo(db, nil)
	}

	err = sd.LoadAssertions(db, commitTo)
	c.Assert(err, IsNil)

	err = sd.LoadMeta(seed.AllModes, nil, timings.New(nil))
	c.Assert(err, IsNil)

	essSnaps = sd.EssentialSnaps()
	runSnaps, err = sd.ModeSnaps("run")
	c.Assert(err, IsNil)

	return essSnaps, runSnaps, db
}

func (s *imageSuite) TestSetupSeed(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	preparedir := c.MkDir()
	rootdir := filepath.Join(preparedir, "image")
	blobdir := filepath.Join(rootdir, "var/lib/snapd/snaps")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	gadgetWriteResolvedContentCalled := 0
	restore = image.MockWriteResolvedContent(func(prepareImageDir string, info *gadget.Info, gadgetRoot, kernelRoot string) error {
		c.Check(prepareImageDir, Equals, preparedir)
		c.Check(gadgetRoot, Equals, filepath.Join(preparedir, "gadget"))
		c.Check(kernelRoot, Equals, filepath.Join(preparedir, "kernel"))
		gadgetWriteResolvedContentCalled++
		return nil
	})
	defer restore()

	opts := &image.Options{
		PrepareDir: preparedir,
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, roDB := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"core", "pc-kernel", "pc"} {
		info := s.AssertedSnapInfo(name)
		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path: p,

			SideInfo: &info.SideInfo,

			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,

			Channel: stableChannel,
		})
		// precondition
		if name == "core" {
			c.Check(essSnaps[i].SideInfo.SnapID, Equals, s.AssertedSnapID("core"))
		}
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path: filepath.Join(seedsnapsdir, s.AssertedSnapInfo("required-snap1").Filename()),

		SideInfo: &s.AssertedSnapInfo("required-snap1").SideInfo,

		Required: true,

		Channel: stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check assertions
	model1, err := s.model.Ref().Resolve(roDB.Find)
	c.Assert(err, IsNil)
	c.Check(model1, DeepEquals, s.model)

	storeAccountKey := s.StoreSigning.StoreAccountKey("")
	brandPubKey := s.Brands.PublicKey("my-brand")
	_, err = roDB.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": storeAccountKey.PublicKeyID(),
	})
	c.Check(err, IsNil)
	_, err = roDB.Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": brandPubKey.ID(),
	})
	c.Check(err, IsNil)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core", "snap_menuentry")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Check(m["snap_core"], Equals, "core_3.snap")
	c.Check(m["snap_menuentry"], Equals, "my display name")

	// check symlinks from snap blob dir
	kernelInfo := s.AssertedSnapInfo("pc-kernel")
	coreInfo := s.AssertedSnapInfo("core")
	kernelBlob := filepath.Join(blobdir, kernelInfo.Filename())
	dst, err := os.Readlink(kernelBlob)
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/pc-kernel_2.snap")
	c.Check(kernelBlob, testutil.FilePresent)

	coreBlob := filepath.Join(blobdir, coreInfo.Filename())
	dst, err = os.Readlink(coreBlob)
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/core_3.snap")
	c.Check(coreBlob, testutil.FilePresent)

	c.Check(s.stderr.String(), Equals, "")

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{4})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})

	// content was resolved and written for ubuntu-image
	c.Check(gadgetWriteResolvedContentCalled, Equals, 1)
}

func (s *imageSuite) TestSetupSeedLocalCoreBrandKernel(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	}, "")

	coreFn := snaptest.MakeTestSnapWithFiles(c, packageCore, [][]string{{"local", ""}})
	requiredSnap1Fn := snaptest.MakeTestSnapWithFiles(c, requiredSnap1, [][]string{{"local", ""}})

	opts := &image.Options{
		Snaps: []string{
			coreFn,
			requiredSnap1Fn,
		},
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, roDB := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"core_x1.snap", "pc-kernel", "pc"} {
		channel := stableChannel
		info := s.AssertedSnapInfo(name)
		var pinfo snap.PlaceInfo = info
		var sideInfo *snap.SideInfo
		var snapType snap.Type
		if info == nil {
			switch name {
			case "core_x1.snap":
				pinfo = snap.MinimalPlaceInfo("core", snap.R(-1))
				sideInfo = &snap.SideInfo{
					RealName: "core",
				}
				channel = ""
				snapType = snap.TypeOS
			}
		} else {
			sideInfo = &info.SideInfo
			snapType = info.Type()
		}

		fn := pinfo.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path: p,

			SideInfo: sideInfo,

			EssentialType: snapType,
			Essential:     true,
			Required:      true,

			Channel: channel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path: filepath.Join(seedsnapsdir, "required-snap1_x1.snap"),

		SideInfo: &snap.SideInfo{
			RealName: "required-snap1",
		},
		Required: true,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	// check assertions
	decls, err := roDB.FindMany(asserts.SnapDeclarationType, nil)
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

func (s *imageSuite) TestSetupSeedWithWideCohort(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	snapFile := snaptest.MakeTestSnapWithFiles(c, devmodeSnap, nil)

	opts := &image.Options{
		Snaps: []string{snapFile},

		PrepareDir:    filepath.Dir(rootdir),
		WideCohortKey: "wide-cohort-key",
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{4})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core",
		Channel:      stableChannel,
		CohortKey:    "wide-cohort-key",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      stableChannel,
		CohortKey:    "wide-cohort-key",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc",
		Channel:      stableChannel,
		CohortKey:    "wide-cohort-key",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[3], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "required-snap1",
		Channel:      stableChannel,
		CohortKey:    "wide-cohort-key",
		Flags:        store.SnapActionIgnoreValidation,
	})
}

func (s *imageSuite) TestSetupSeedDevmodeSnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	snapFile := snaptest.MakeTestSnapWithFiles(c, devmodeSnap, nil)

	opts := &image.Options{
		Snaps: []string{snapFile},

		PrepareDir: filepath.Dir(rootdir),
		Channel:    "beta",
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 2)

	for i, name := range []string{"core", "pc-kernel", "pc"} {
		info := s.AssertedSnapInfo(name)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          filepath.Join(seedsnapsdir, info.Filename()),
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       "beta",
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap1_3.snap"),
		SideInfo: &s.AssertedSnapInfo("required-snap1").SideInfo,
		Required: true,
		Channel:  "beta",
	})
	// ensure local snaps are put last in seed.yaml
	c.Check(runSnaps[1], DeepEquals, &seed.Snap{
		Path: filepath.Join(seedsnapsdir, "devmode-snap_x1.snap"),
		SideInfo: &snap.SideInfo{
			RealName: "devmode-snap",
		},
		DevMode: true,
		// no channel for unasserted snaps
		Channel: "",
	})
	// check devmode-snap blob
	c.Check(runSnaps[1].Path, testutil.FilePresent)
}

func (s *imageSuite) TestSetupSeedWithClassicSnapFails(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	s.MakeAssertedSnap(c, classicSnap, nil, snap.R(1), "other")

	opts := &image.Options{
		Snaps: []string{s.AssertedSnap("classic-snap")},

		PrepareDir: filepath.Dir(rootdir),
		Channel:    "beta",
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	blobdir := filepath.Join(rootdir, "/var/lib/snapd/snaps")
	s.setupSnaps(c, map[string]string{
		"core18":     "canonical",
		"pc18":       "canonical",
		"pc-kernel":  "canonical",
		"snapd":      "canonical",
		"other-base": "other",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"snapd", "core18_18.snap", "pc-kernel", "pc18"} {
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   s.AssertedSnapID("core18"),
						RealName: "core18",
						Revision: snap.R("18"),
					},
					SnapType: snap.TypeBase,
				}
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "other-base_18.snap"),
		SideInfo: &s.AssertedSnapInfo("other-base").SideInfo,
		Required: true,
		Channel:  stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 5)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core18_18.snap")

	// check symlinks from snap blob dir
	kernelInfo := s.AssertedSnapInfo("pc-kernel")
	baseInfo := s.AssertedSnapInfo("core18")
	kernelBlob := filepath.Join(blobdir, kernelInfo.Filename())
	dst, err := os.Readlink(kernelBlob)
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/pc-kernel_2.snap")
	c.Check(kernelBlob, testutil.FilePresent)

	baseBlob := filepath.Join(blobdir, baseInfo.Filename())
	dst, err = os.Readlink(baseBlob)
	c.Assert(err, IsNil)
	c.Check(dst, Equals, "../seed/snaps/core18_18.snap")
	c.Check(baseBlob, testutil.FilePresent)

	c.Check(s.stderr.String(), Equals, "")

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{5})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "snapd",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core18",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[3], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc18",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
}

func (s *imageSuite) TestSetupSeedWithBaseWithCloudConf(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc18",
		"kernel":       "pc-kernel",
		"base":         "core18",
	})

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc-kernel": "canonical",
		"snapd":     "canonical",
	}, "")
	s.MakeAssertedSnap(c, packageGadgetWithBase, [][]string{
		{"grub.conf", ""},
		{"grub.cfg", "I'm a grub.cfg"},
		{"cloud.conf", "# cloud config"},
		{"meta/gadget.yaml", pcGadgetYaml},
	}, snap.R(5), "canonical")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	c.Check(filepath.Join(rootdir, "/etc/cloud/cloud.cfg"), testutil.FileEquals, "# cloud config")
}

func (s *imageSuite) TestSetupSeedWithBaseWithCustomizations(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc18",
		"kernel":       "pc-kernel",
		"base":         "core18",
	})

	tmpdir := c.MkDir()
	rootdir := filepath.Join(tmpdir, "image")
	cloudInitUserData := filepath.Join(tmpdir, "cloudstuff")
	err := ioutil.WriteFile(cloudInitUserData, []byte(`# user cloud data`), 0644)
	c.Assert(err, IsNil)
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc-kernel": "canonical",
		"snapd":     "canonical",
	}, "")
	s.MakeAssertedSnap(c, packageGadgetWithBase, [][]string{
		{"grub.conf", ""},
		{"grub.cfg", "I'm a grub.cfg"},
		{"meta/gadget.yaml", pcGadgetYaml},
	}, snap.R(5), "canonical")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: cloudInitUserData,
		},
	}

	err = image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check customization impl files were written
	varCloudDir := filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net")
	c.Check(filepath.Join(varCloudDir, "meta-data"), testutil.FileEquals, "instance-id: nocloud-static\n")
	c.Check(filepath.Join(varCloudDir, "user-data"), testutil.FileEquals, "# user cloud data")
	// console-conf disable
	c.Check(filepath.Join(rootdir, "_writable_defaults", "var/lib/console-conf/complete"), testutil.FilePresent)
}

func (s *imageSuite) TestPrepareUC20CustomizationsUnsupported(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.makeUC20Model(nil)
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
		},
	})
	c.Assert(err, ErrorMatches, `cannot support with UC20\+ model requested customizations: console-conf disable, cloud-init user-data`)
}

func (s *imageSuite) TestPrepareClassicCustomizationsUnsupported(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
	})
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		Classic:   true,
		ModelFile: fn,
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
			BootFlags:         []string{"boot-flag"},
		},
	})
	c.Assert(err, ErrorMatches, `cannot support with classic model requested customizations: console-conf disable, boot flags \(boot-flag\)`)
}

func (s *imageSuite) TestPrepareUC18CustomizationsUnsupported(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc18",
		"kernel":       "pc-kernel",
		"base":         "core18",
	})
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile: fn,
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
			BootFlags:         []string{"boot-flag"},
		},
	})
	c.Assert(err, ErrorMatches, `cannot support with UC16/18 model requested customizations: boot flags \(boot-flag\)`)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
		"snapd":     "canonical",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 2)

	// check the files are in place
	for i, name := range []string{"snapd", "core18_18.snap", "pc-kernel", "pc18"} {
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   s.AssertedSnapID("core18"),
						RealName: "core18",
						Revision: snap.R("18"),
					},
					SnapType: snap.TypeBase,
				}
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, s.AssertedSnapInfo("core").Filename()),
		SideInfo: &s.AssertedSnapInfo("core").SideInfo,
		Required: false, // strange but expected
		Channel:  stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)
	c.Check(runSnaps[1], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, s.AssertedSnapInfo("required-snap1").Filename()),
		SideInfo: &s.AssertedSnapInfo("required-snap1").SideInfo,
		Required: true,
		Channel:  stableChannel,
	})
	c.Check(runSnaps[1].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 6)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core18_18.snap")

	c.Check(s.stderr.String(), Equals, "WARNING: model has base \"core18\" but some snaps (\"required-snap1\") require \"core\" as base as well, for compatibility it was added implicitly, adding \"core\" explicitly is recommended\n")

	// current snap info sent
	c.Check(s.curSnaps, HasLen, 2)
	c.Check(s.curSnaps[0], HasLen, 0)
	c.Check(s.curSnaps[1], DeepEquals, []*store.CurrentSnap{
		{
			InstanceName:     "snapd",
			SnapID:           s.AssertedSnapID("snapd"),
			Revision:         snap.R(18),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
		{
			InstanceName:     "pc-kernel",
			SnapID:           s.AssertedSnapID("pc-kernel"),
			Revision:         snap.R(2),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
		{
			InstanceName:     "core18",
			SnapID:           s.AssertedSnapID("core18"),
			Revision:         snap.R(18),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
		{
			InstanceName:     "pc18",
			SnapID:           s.AssertedSnapID("pc18"),
			Revision:         snap.R(4),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
		{
			InstanceName:     "required-snap1",
			SnapID:           s.AssertedSnapID("required-snap1"),
			Revision:         snap.R(3),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
	})
}

func (s *imageSuite) TestSetupSeedWithBaseDefaultTrackSnap(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc18",
		"kernel":         "pc-kernel",
		"base":           "core18",
		"required-snaps": []interface{}{"default-track-snap18"},
	})

	// default-track-snap18 has a default-track

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
		"snapd":     "canonical",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"snapd", "core18_18.snap", "pc-kernel", "pc18"} {
		info := s.AssertedSnapInfo(name)
		if info == nil {
			switch name {
			case "core18_18.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						SnapID:   s.AssertedSnapID("core18"),
						RealName: "core18",
						Revision: snap.R("18"),
					},
					SnapType: snap.TypeBase,
				}
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, s.AssertedSnapInfo("default-track-snap18").Filename()),
		SideInfo: &s.AssertedSnapInfo("default-track-snap18").SideInfo,
		Required: true,
		Channel:  "default-track/stable",
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 5)

	c.Check(s.stderr.String(), Equals, "")
}

func (s *imageSuite) TestSetupSeedKernelPublisherMismatch(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "other",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, ErrorMatches, `cannot use kernel "pc-kernel" published by "other" for model by "my-brand"`)
}

func (s *imageSuite) TestInstallCloudConfigNoConfig(c *C) {
	targetDir := c.MkDir()
	emptyGadgetDir := c.MkDir()

	err := image.InstallCloudConfig(targetDir, emptyGadgetDir)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(filepath.Join(targetDir, "etc/cloud")), Equals, false)
}

func (s *imageSuite) TestInstallCloudConfigWithCloudConfig(c *C) {
	canary := []byte("ni! ni! ni!")

	targetDir := c.MkDir()
	gadgetDir := c.MkDir()
	err := ioutil.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), canary, 0644)
	c.Assert(err, IsNil)

	err = image.InstallCloudConfig(targetDir, gadgetDir)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(targetDir, "etc/cloud/cloud.cfg"), testutil.FileEquals, canary)
}

func (s *imageSuite) TestSetupSeedLocalSnapsWithStoreAsserts(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	}, "")

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("core"),
			s.AssertedSnap("required-snap1"),
		},
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, roDB := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc"} {
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
					SnapType: snap.TypeOS,
				}
			default:
				c.Errorf("cannot have %s", name)
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap1_3.snap"),
		Required: true,
		SideInfo: &snap.SideInfo{
			RealName: "required-snap1",
			SnapID:   s.AssertedSnapID("required-snap1"),
			Revision: snap.R(3),
		},
		Channel: stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check assertions
	decls, err := roDB.FindMany(asserts.SnapDeclarationType, nil)
	c.Assert(err, IsNil)
	c.Check(decls, HasLen, 4)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core_3.snap")

	c.Check(s.stderr.String(), Equals, `WARNING: proceeding to download snaps ignoring validations, this default will change in the future. For now use --validation=enforce for validations to be taken into account, pass instead --validation=ignore to preserve current behavior going forward`+"\n")

	// current snap info sent
	c.Check(s.curSnaps, HasLen, 1)
	c.Check(s.curSnaps[0], DeepEquals, []*store.CurrentSnap{
		{
			InstanceName:     "core",
			SnapID:           s.AssertedSnapID("core"),
			Revision:         snap.R(3),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
		{
			InstanceName:     "required-snap1",
			SnapID:           s.AssertedSnapID("required-snap1"),
			Revision:         snap.R(3),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: true,
		},
	})
}

func (s *imageSuite) TestSetupSeedLocalSnapsWithChannels(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	}, "")

	opts := &image.Options{
		Snaps: []string{
			"core",
			s.AssertedSnap("required-snap1"),
		},
		PrepareDir: filepath.Dir(rootdir),
		SnapChannels: map[string]string{
			"core": "candidate",
			// keep this comment for gofmt 1.9
			s.AssertedSnap("required-snap1"): "edge",
		},
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc"} {
		info := s.AssertedSnapInfo(name)
		channel := stableChannel
		if info == nil {
			switch name {
			case "core_3.snap":
				info = &snap.Info{
					SideInfo: snap.SideInfo{
						RealName: "core",
						SnapID:   s.AssertedSnapID("core"),
						Revision: snap.R(3),
					},
					SnapType: snap.TypeOS,
				}
				channel = "candidate"
			default:
				c.Errorf("cannot have %s", name)
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       channel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap1_3.snap"),
		Required: true,
		SideInfo: &snap.SideInfo{
			RealName: "required-snap1",
			SnapID:   s.AssertedSnapID("required-snap1"),
			Revision: snap.R(3),
		},
		Channel: "edge",
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)
}

func (s *imageSuite) TestSetupSeedLocalSnapsWithStoreAssertsValidationEnforce(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc":        "canonical",
		"pc-kernel": "my-brand",
	}, "")

	opts := &image.Options{
		Snaps: []string{
			s.AssertedSnap("pc"),
		},
		PrepareDir: filepath.Dir(rootdir),
		Customizations: image.Customizations{
			Validation: "enforce",
		},
	}

	err := image.SetupSeed(s.tsto, s.model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, roDB := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"core_3.snap", "pc-kernel", "pc"} {
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
					SnapType: snap.TypeOS,
				}
			default:
				c.Errorf("cannot have %s", name)
			}
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap1_3.snap"),
		Required: true,
		SideInfo: &snap.SideInfo{
			RealName:      "required-snap1",
			SnapID:        s.AssertedSnapID("required-snap1"),
			Revision:      snap.R(3),
			EditedContact: "mailto:foo@example.com",
		},
		Channel: stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check assertions
	decls, err := roDB.FindMany(asserts.SnapDeclarationType, nil)
	c.Assert(err, IsNil)
	c.Check(decls, HasLen, 4)

	// check the bootloader config
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m["snap_kernel"], Equals, "pc-kernel_2.snap")
	c.Assert(err, IsNil)
	c.Check(m["snap_core"], Equals, "core_3.snap")

	c.Check(s.stderr.String(), Equals, "")

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{3})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core",
		Channel:      stableChannel,
		Flags:        store.SnapActionEnforceValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      stableChannel,
		Flags:        store.SnapActionEnforceValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "required-snap1",
		Channel:      stableChannel,
		Flags:        store.SnapActionEnforceValidation,
	})
	c.Check(s.curSnaps, HasLen, 1)
	c.Check(s.curSnaps[0], DeepEquals, []*store.CurrentSnap{
		{
			InstanceName:     "pc",
			SnapID:           s.AssertedSnapID("pc"),
			Revision:         snap.R(1),
			TrackingChannel:  "stable",
			Epoch:            snap.E("0"),
			IgnoreValidation: false,
		},
	})
}

func (s *imageSuite) TestCannotCreateGadgetUnpackDir(c *C) {
	fn := filepath.Join(c.MkDir(), "model.assertion")
	err := ioutil.WriteFile(fn, asserts.Encode(s.model), 0644)
	c.Assert(err, IsNil)

	err = image.Prepare(&image.Options{
		ModelFile:  fn,
		Channel:    "stable",
		PrepareDir: "/no-where",
	})
	c.Assert(err, ErrorMatches, `cannot create unpack dir "/no-where/gadget": mkdir .*`)
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
	c.Assert(err, ErrorMatches, `cannot use global default option channel: channel name has too many components: x/x/x/x`)
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

func (s *imageSuite) TestPrepareClassicModelNoModelAssertion(c *C) {
	preparedir := c.MkDir()
	s.setupSnaps(c, nil, "")

	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	restore = sysdb.MockGenericClassicModel(s.StoreSigning.GenericClassicModel)
	defer restore()
	restore = image.MockNewToolingStoreFromModel(func(model *asserts.Model, fallbackArchitecture string) (*tooling.ToolingStore, error) {
		return s.tsto, nil
	})
	defer restore()

	// prepare an image with no model assertion but classic set to true
	// to ensure the GenericClassicModel is used without error
	err := image.Prepare(&image.Options{
		Architecture: "amd64",
		PrepareDir:   preparedir,
		Classic:      true,
		Snaps:        []string{"required-snap18", "core18"},
	})
	c.Assert(err, IsNil)

	// ensure the prepareDir was preseeded
	seeddir := filepath.Join(preparedir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	c.Check(filepath.Join(seeddir, "seed.yaml"), testutil.FilePresent)
	m, err := filepath.Glob(filepath.Join(seedsnapsdir, "*"))
	c.Assert(err, IsNil)
	// generic classic model has no other snaps, so we expect only the snaps
	// that were passed in options to be present
	c.Check(m, DeepEquals, []string{
		filepath.Join(seedsnapsdir, "core18_18.snap"),
		filepath.Join(seedsnapsdir, "required-snap18_6.snap"),
	})
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Channel:    "stable",
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 0)

	c.Check(essSnaps[0], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "core_3.snap"),
		SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
		EssentialType: snap.TypeOS,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	})
	c.Check(essSnaps[1], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "pc-kernel_2.snap"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18/stable",
	})
	c.Check(essSnaps[2], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "pc_1.snap"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "18/stable",
	})

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{3})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core",
		Channel:      "stable",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      "18/stable",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc",
		Channel:      "18/stable",
		Flags:        store.SnapActionIgnoreValidation,
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

	s.setupSnaps(c, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	rootdir := filepath.Join(c.MkDir(), "image")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Channel:    "edge",
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 0)

	c.Check(essSnaps[0], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "core_3.snap"),
		SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
		EssentialType: snap.TypeOS,
		Essential:     true,
		Required:      true,
		Channel:       "edge",
	})
	c.Check(essSnaps[1], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "pc-kernel_2.snap"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18/edge",
	})
	c.Check(essSnaps[2], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "pc_1.snap"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "edge",
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")

	// pretend we downloaded the core,kernel already
	cfn := s.AssertedSnap("core")
	kfn := s.AssertedSnap("pc-kernel")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Snaps:      []string{kfn, cfn},
		Channel:    "beta",
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 0)

	c.Check(essSnaps[0], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "core_3.snap"),
		SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
		EssentialType: snap.TypeOS,
		Essential:     true,
		Required:      true,
		Channel:       "beta",
	})
	c.Check(essSnaps[1], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "pc-kernel_2.snap"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18/beta",
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	}, "")

	coreFn := snaptest.MakeTestSnapWithFiles(c, packageCore, [][]string{{"local", ""}})

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Snaps: []string{
			coreFn,
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 2)

	c.Check(essSnaps[0].Path, Equals, filepath.Join(seedsnapsdir, "snapd_18.snap"))
	c.Check(essSnaps[1].Path, Equals, filepath.Join(seedsnapsdir, "core18_18.snap"))
	c.Check(essSnaps[2].Path, Equals, filepath.Join(seedsnapsdir, "pc-kernel_2.snap"))
	c.Check(essSnaps[3].Path, Equals, filepath.Join(seedsnapsdir, "pc18_4.snap"))

	c.Check(runSnaps[0].Path, Equals, filepath.Join(seedsnapsdir, "core_x1.snap"))
	c.Check(runSnaps[1].Path, Equals, filepath.Join(seedsnapsdir, "required-snap1_3.snap"))
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"core":      "canonical",
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	}, "")

	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 2)

	c.Check(essSnaps[0].Path, Equals, filepath.Join(seedsnapsdir, "snapd_18.snap"))
	c.Check(essSnaps[1].Path, Equals, filepath.Join(seedsnapsdir, "core18_18.snap"))
	c.Check(essSnaps[2].Path, Equals, filepath.Join(seedsnapsdir, "pc-kernel_2.snap"))
	c.Check(essSnaps[3].Path, Equals, filepath.Join(seedsnapsdir, "pc18_4.snap"))

	c.Check(runSnaps[0].Path, Equals, filepath.Join(seedsnapsdir, "core_3.snap"))
	c.Check(runSnaps[1].Path, Equals, filepath.Join(seedsnapsdir, "required-snap1_3.snap"))
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core18":    "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":           "canonical",
		"pc":             "canonical",
		"pc-kernel":      "canonical",
		"snap-base-none": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	c.Assert(image.SetupSeed(s.tsto, model, opts), IsNil)
}

func (s *imageSuite) TestSetupSeedCore18GadgetDefaults(c *C) {
	systemctlMock := testutil.MockCommand(c, "systemctl", "")
	defer systemctlMock.Restore()

	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// replace model with a model that uses core18
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "pc18",
		"kernel":       "pc-kernel",
		"base":         "core18",
	})

	defaults := `defaults:
  system:
       service:
         ssh:
           disable: true
`

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	}, defaults)

	snapdFn := snaptest.MakeTestSnapWithFiles(c, snapdSnap, [][]string{{"local", ""}})
	core18Fn := snaptest.MakeTestSnapWithFiles(c, packageCore18, [][]string{{"local", ""}})

	opts := &image.Options{
		Snaps: []string{
			snapdFn,
			core18Fn,
		},

		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(filepath.Join(rootdir, "_writable_defaults/etc/ssh/sshd_not_to_be_run")), Equals, true)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":      "canonical",
		"pc":        "canonical",
		"pc-kernel": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err = image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	essSnaps, runSnaps, roDB := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 0)

	// check the store assertion was fetched
	_, err = roDB.Find(asserts.StoreType, map[string]string{
		"store": "my-store",
	})
	c.Check(err, IsNil)
}

func (s *imageSuite) TestSetupSeedSnapReqBaseFromLocal(c *C) {
	// As originally written it let an extra snap fullfil
	// the prereq of a required one, this does not work anymore!
	// See TestSetupSeedSnapReqBaseFromExtraFails.
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"other-base", "snap-req-other-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
		"other-base":          "canonical",
	}, "")
	bfn := s.AssertedSnap("other-base")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Snaps:      []string{bfn},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)
}

func (s *imageSuite) TestSetupSeedSnapReqBaseFromExtraFails(c *C) {
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"snap-req-other-base"},
	})

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":                "canonical",
		"pc":                  "canonical",
		"pc-kernel":           "canonical",
		"snap-req-other-base": "canonical",
		"other-base":          "canonical",
	}, "")
	bfn := s.AssertedSnap("other-base")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
		Snaps:      []string{bfn},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Check(err, ErrorMatches, `cannot add snap "snap-req-other-base" without also adding its base "other-base" explicitly`)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"core":                  "canonical",
		"pc":                    "canonical",
		"pc-kernel":             "canonical",
		"snap-req-content-snap": "canonical",
	}, "")
	opts := &image.Options{
		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Check(err, ErrorMatches, `cannot use snap "snap-req-content-provider" without its default content provider "gtk-common-themes" being added explicitly`)
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

	rootdir := c.MkDir()
	s.setupSnaps(c, map[string]string{
		"classic-gadget": "my-brand",
	}, "")

	opts := &image.Options{
		Classic:    true,
		PrepareDir: rootdir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 2)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	c.Check(essSnaps[0], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "core_3.snap"),
		SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
		EssentialType: snap.TypeOS,
		Essential:     true,
		Required:      true,
		Channel:       stableChannel,
	})
	c.Check(essSnaps[0].Path, testutil.FilePresent)
	c.Check(essSnaps[1], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "classic-gadget_5.snap"),
		SideInfo:      &s.AssertedSnapInfo("classic-gadget").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       stableChannel,
	})
	c.Check(essSnaps[1].Path, testutil.FilePresent)
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap1_3.snap"),
		SideInfo: &s.AssertedSnapInfo("required-snap1").SideInfo,
		Required: true,
		Channel:  stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 3)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})

	c.Check(s.stderr.String(), Matches, `WARNING: ensure that the contents under .*/var/lib/snapd/seed are owned by root:root in the \(final\) image\n`)

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

	rootdir := c.MkDir()
	s.setupSnaps(c, nil, "")

	snapFile := snaptest.MakeTestSnapWithFiles(c, classicSnap, nil)

	opts := &image.Options{
		Classic:    true,
		Snaps:      []string{snapFile},
		PrepareDir: rootdir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 1)
	c.Check(runSnaps, HasLen, 1)

	c.Check(essSnaps[0], DeepEquals, &seed.Snap{
		Path:          filepath.Join(seedsnapsdir, "core_3.snap"),
		SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
		EssentialType: snap.TypeOS,
		Essential:     true,
		Required:      true,
		Channel:       stableChannel,
	})
	c.Check(essSnaps[0].Path, testutil.FilePresent)

	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path: filepath.Join(seedsnapsdir, "classic-snap_x1.snap"),
		SideInfo: &snap.SideInfo{
			RealName: "classic-snap",
		},
		Classic: true,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
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

	rootdir := c.MkDir()
	s.setupSnaps(c, map[string]string{
		"classic-gadget18": "my-brand",
	}, "")

	opts := &image.Options{
		Classic:    true,
		PrepareDir: rootdir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 3)
	c.Check(runSnaps, HasLen, 1)

	// check the files are in place
	for i, name := range []string{"snapd", "classic-gadget18", "core18"} {
		info := s.AssertedSnapInfo(name)

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       stableChannel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required-snap18_6.snap"),
		SideInfo: &s.AssertedSnapInfo("required-snap18").SideInfo,
		Required: true,
		Channel:  stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check that the  bootloader is unset
	m, err := s.bootloader.GetBootVars("snap_kernel", "snap_core")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snap_core":   "",
		"snap_kernel": "",
	})

	c.Check(s.stderr.String(), Matches, `WARNING: ensure that the contents under .*/var/lib/snapd/seed are owned by root:root in the \(final\) image\n`)

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

	rootdir := c.MkDir()

	opts := &image.Options{
		Classic:    true,
		PrepareDir: rootdir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(rootdir, "var/lib/snapd/seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 0)
	c.Check(runSnaps, HasLen, 0)

	l, err := ioutil.ReadDir(seedsnapsdir)
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

	rootdir := c.MkDir()
	s.setupSnaps(c, map[string]string{
		"classic-gadget18": "my-brand",
	}, "")

	opts := &image.Options{
		Classic:    true,
		PrepareDir: rootdir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, ErrorMatches, `cannot use "snap-req-core16-base" requiring base "core16" without adding "core16" \(or "core"\) explicitly`)
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

	rootdir := filepath.Join(c.MkDir(), "image")
	s.setupSnaps(c, map[string]string{
		"pc18":      "canonical",
		"pc-kernel": "canonical",
	}, "")

	snapdFn := snaptest.MakeTestSnapWithFiles(c, snapdSnap, [][]string{{"local", ""}})
	core18Fn := snaptest.MakeTestSnapWithFiles(c, packageCore18, [][]string{{"local", ""}})

	opts := &image.Options{
		Snaps: []string{
			snapdFn,
			core18Fn,
		},

		PrepareDir: filepath.Dir(rootdir),
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)
	c.Assert(s.stdout.String(), Matches, `(?ms).*Copying ".*/snapd_3.14_all.snap" \(snapd\)`)
}

func (s *imageSuite) TestCore20MakeLabel(c *C) {
	c.Check(image.MakeLabel(time.Date(2019, 10, 30, 0, 0, 0, 0, time.UTC)), Equals, "20191030")
}

func (s *imageSuite) makeSnap(c *C, yamlKey string, files [][]string, revno snap.Revision, publisher string) {
	if publisher == "" {
		publisher = "canonical"
	}
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml[yamlKey], files, revno, publisher)
}

func (s *imageSuite) makeUC20Model(extraHeaders map[string]interface{}) *asserts.Model {
	headers := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "required20",
				"id":   s.AssertedSnapID("required20"),
			}},
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	return s.Brands.Model("my-brand", "my-model", headers)
}

func (s *imageSuite) TestSetupSeedCore20Grub(c *C) {
	bootloader.Force(nil)
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// a model that uses core20
	model := s.makeUC20Model(nil)

	prepareDir := c.MkDir()

	s.makeSnap(c, "snapd", nil, snap.R(1), "")
	s.makeSnap(c, "core20", nil, snap.R(20), "")
	s.makeSnap(c, "pc-kernel=20", nil, snap.R(1), "")
	gadgetContent := [][]string{
		{"grub-recovery.conf", "# recovery grub.cfg"},
		{"grub.conf", "# boot grub.cfg"},
		{"meta/gadget.yaml", pcUC20GadgetYaml},
	}
	s.makeSnap(c, "pc=20", gadgetContent, snap.R(22), "")
	s.makeSnap(c, "required20", nil, snap.R(21), "other")

	opts := &image.Options{
		PrepareDir: prepareDir,
		Customizations: image.Customizations{
			BootFlags:  []string{"factory"},
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// check seed
	seeddir := filepath.Join(prepareDir, "system-seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 1)

	stableChannel := "latest/stable"

	// check the files are in place
	for i, name := range []string{"snapd", "pc-kernel", "core20", "pc"} {
		info := s.AssertedSnapInfo(name)

		channel := stableChannel
		switch name {
		case "pc", "pc-kernel":
			channel = "20"
		}

		fn := info.Filename()
		p := filepath.Join(seedsnapsdir, fn)
		c.Check(p, testutil.FilePresent)
		c.Check(essSnaps[i], DeepEquals, &seed.Snap{
			Path:          p,
			SideInfo:      &info.SideInfo,
			EssentialType: info.Type(),
			Essential:     true,
			Required:      true,
			Channel:       channel,
		})
	}
	c.Check(runSnaps[0], DeepEquals, &seed.Snap{
		Path:     filepath.Join(seedsnapsdir, "required20_21.snap"),
		SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
		Required: true,
		Channel:  stableChannel,
	})
	c.Check(runSnaps[0].Path, testutil.FilePresent)

	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 5)

	// check boot config
	grubCfg := filepath.Join(prepareDir, "system-seed", "EFI/ubuntu/grub.cfg")
	seedGrubenv := filepath.Join(prepareDir, "system-seed", "EFI/ubuntu/grubenv")
	grubRecoveryCfgAsset := assets.Internal("grub-recovery.cfg")
	c.Assert(grubRecoveryCfgAsset, NotNil)
	c.Check(grubCfg, testutil.FileEquals, string(grubRecoveryCfgAsset))
	// make sure that grub.cfg and grubenv are the only files present inside
	// the directory
	gl, err := filepath.Glob(filepath.Join(prepareDir, "system-seed/EFI/ubuntu/*"))
	c.Assert(err, IsNil)
	c.Check(gl, DeepEquals, []string{
		grubCfg,
		seedGrubenv,
	})

	// check recovery system specific config
	systems, err := filepath.Glob(filepath.Join(seeddir, "systems", "*"))
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 1)

	seedGenv := grubenv.NewEnv(seedGrubenv)
	c.Assert(seedGenv.Load(), IsNil)
	c.Check(seedGenv.Get("snapd_recovery_system"), Equals, filepath.Base(systems[0]))
	c.Check(seedGenv.Get("snapd_recovery_mode"), Equals, "install")
	c.Check(seedGenv.Get("snapd_boot_flags"), Equals, "factory")

	c.Check(s.stderr.String(), Equals, "")

	systemGenv := grubenv.NewEnv(filepath.Join(systems[0], "grubenv"))
	c.Assert(systemGenv.Load(), IsNil)
	c.Check(systemGenv.Get("snapd_recovery_kernel"), Equals, "/snaps/pc-kernel_1.snap")

	// check the downloads
	c.Check(s.storeActionsBunchSizes, DeepEquals, []int{5})
	c.Check(s.storeActions[0], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "snapd",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[1], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc-kernel",
		Channel:      "20",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[2], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "core20",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[3], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "pc",
		Channel:      "20",
		Flags:        store.SnapActionIgnoreValidation,
	})
	c.Check(s.storeActions[4], DeepEquals, &store.SnapAction{
		Action:       "download",
		InstanceName: "required20",
		Channel:      stableChannel,
		Flags:        store.SnapActionIgnoreValidation,
	})
}

func (s *imageSuite) TestSetupSeedCore20UBoot(c *C) {
	bootloader.Force(nil)
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// a model that uses core20 and our gadget
	headers := map[string]interface{}{
		"display-name": "my model",
		"architecture": "arm64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "arm-kernel",
				"id":              s.AssertedSnapID("arm-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "uboot-gadget",
				"id":              s.AssertedSnapID("uboot-gadget"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}
	model := s.Brands.Model("my-brand", "my-model", headers)

	prepareDir := c.MkDir()

	s.makeSnap(c, "snapd", nil, snap.R(1), "")
	s.makeSnap(c, "core20", nil, snap.R(20), "")
	kernelContent := [][]string{
		{"kernel.img", "some kernel"},
		{"initrd.img", "some initrd"},
		{"dtbs/foo.dtb", "some dtb"},
	}
	s.makeSnap(c, "arm-kernel=20", kernelContent, snap.R(1), "")
	gadgetContent := [][]string{
		// this file must be empty
		// TODO:UC20: write this test with non-empty uboot.env when we support
		//            that
		{"uboot.conf", ""},
		{"meta/gadget.yaml", piUC20GadgetYaml},
	}
	s.makeSnap(c, "uboot-gadget=20", gadgetContent, snap.R(22), "")

	opts := &image.Options{
		PrepareDir: prepareDir,
		Customizations: image.Customizations{
			BootFlags: []string{"factory"},
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, IsNil)

	// validity checks
	seeddir := filepath.Join(prepareDir, "system-seed")
	seedsnapsdir := filepath.Join(seeddir, "snaps")
	essSnaps, runSnaps, _ := s.loadSeed(c, seeddir)
	c.Check(essSnaps, HasLen, 4)
	c.Check(runSnaps, HasLen, 0)
	l, err := ioutil.ReadDir(seedsnapsdir)
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// check boot config

	// uboot.env will be missing
	ubootEnv := filepath.Join(prepareDir, "system-seed", "uboot.env")
	c.Check(ubootEnv, testutil.FileAbsent)

	// boot.sel will be present and have snapd_recovery_system set
	expectedLabel := image.MakeLabel(time.Now())
	bootSel := filepath.Join(prepareDir, "system-seed", "uboot", "ubuntu", "boot.sel")

	env, err := ubootenv.Open(bootSel)
	c.Assert(err, IsNil)
	c.Assert(env.Get("snapd_recovery_system"), Equals, expectedLabel)
	c.Assert(env.Get("snapd_recovery_mode"), Equals, "install")
	c.Assert(env.Get("snapd_boot_flags"), Equals, "factory")

	// check recovery system specific config
	systems, err := filepath.Glob(filepath.Join(seeddir, "systems", "*"))
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 1)
	c.Check(filepath.Base(systems[0]), Equals, expectedLabel)

	// check we extracted the kernel assets
	for _, fileAndContent := range kernelContent {
		file := fileAndContent[0]
		content := fileAndContent[1]
		c.Assert(filepath.Join(systems[0], "kernel", file), testutil.FileEquals, content)
	}
}

func (s *imageSuite) TestSetupSeedCore20NoKernelRefsConsumed(c *C) {
	bootloader.Force(nil)
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// a model that uses core20 and our gadget
	headers := map[string]interface{}{
		"display-name": "my model",
		"architecture": "arm64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "arm-kernel",
				"id":              s.AssertedSnapID("arm-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "uboot-gadget",
				"id":              s.AssertedSnapID("uboot-gadget"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}
	model := s.Brands.Model("my-brand", "my-model", headers)

	prepareDir := c.MkDir()

	s.makeSnap(c, "snapd", nil, snap.R(1), "")
	s.makeSnap(c, "core20", nil, snap.R(20), "")
	kernelYaml := `
assets:
 ref:
  update: true
  content:
   - dtbs/`
	kernelContent := [][]string{
		{"meta/kernel.yaml", kernelYaml},
		{"kernel.img", "some kernel"},
		{"initrd.img", "some initrd"},
		{"dtbs/foo.dtb", "some dtb"},
	}
	s.makeSnap(c, "arm-kernel=20", kernelContent, snap.R(1), "")
	gadgetContent := [][]string{
		// this file must be empty
		// TODO:UC20: write this test with non-empty uboot.env when we support
		//            that
		{"uboot.conf", ""},
		{"meta/gadget.yaml", piUC20GadgetYaml},
	}
	s.makeSnap(c, "uboot-gadget=20", gadgetContent, snap.R(22), "")

	opts := &image.Options{
		PrepareDir: prepareDir,
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Assert(err, ErrorMatches, `no asset from the kernel.yaml needing synced update is consumed by the gadget at "/.*"`)
}

func (s *imageSuite) TestPrepareWithUC20Preseed(c *C) {
	restoreSetupSeed := image.MockSetupSeed(func(tsto *tooling.ToolingStore, model *asserts.Model, opts *image.Options) error {
		return nil
	})
	defer restoreSetupSeed()

	var preseedCalled bool
	restorePreseedCore20 := image.MockPreseedCore20(func(dir, key, aaDir string) error {
		preseedCalled = true
		c.Assert(dir, Equals, "/a/dir")
		c.Assert(key, Equals, "foo")
		c.Assert(aaDir, Equals, "/custom/aa/features")
		return nil
	})
	defer restorePreseedCore20()

	model := s.makeUC20Model(nil)
	fn := filepath.Join(c.MkDir(), "model.assertion")
	c.Assert(ioutil.WriteFile(fn, asserts.Encode(model), 0644), IsNil)

	err := image.Prepare(&image.Options{
		ModelFile:      fn,
		Preseed:        true,
		PrepareDir:     "/a/dir",
		PreseedSignKey: "foo",

		AppArmorKernelFeaturesDir: "/custom/aa/features",
	})
	c.Assert(err, IsNil)
	c.Check(preseedCalled, Equals, true)
}

func (s *imageSuite) TestPrepareWithClassicPreseedError(c *C) {
	restoreSetupSeed := image.MockSetupSeed(func(tsto *tooling.ToolingStore, model *asserts.Model, opts *image.Options) error {
		return nil
	})
	defer restoreSetupSeed()

	err := image.Prepare(&image.Options{
		Preseed:    true,
		Classic:    true,
		PrepareDir: "/a/dir",
	})
	c.Assert(err, ErrorMatches, `cannot preseed the image for a classic model`)
}

func (s *imageSuite) TestSetupSeedCore20DelegatedSnap(c *C) {
	bootloader.Force(nil)
	restore := image.MockTrusted(s.StoreSigning.Trusted)
	defer restore()

	// a model that uses core20
	model := s.makeUC20Model(nil)

	prepareDir := c.MkDir()

	s.makeSnap(c, "snapd", nil, snap.R(1), "")
	s.makeSnap(c, "core20", nil, snap.R(20), "")
	s.makeSnap(c, "pc-kernel=20", nil, snap.R(1), "")
	gadgetContent := [][]string{
		{"grub.conf", "# boot grub.cfg"},
		{"meta/gadget.yaml", pcUC20GadgetYaml},
	}
	s.makeSnap(c, "pc=20", gadgetContent, snap.R(22), "")

	ra := map[string]interface{}{
		"account-id": "my-brand",
		"provenance": []interface{}{"delegated-prov"},
	}
	s.MakeAssertedDelegatedSnap(c, seedtest.SampleSnapYaml["required20"]+"\nprovenance: delegated-prov\n", nil, snap.R(1), "my-brand", "my-brand", "delegated-prov", ra, s.StoreSigning.Database)

	opts := &image.Options{
		PrepareDir: prepareDir,
		Customizations: image.Customizations{
			BootFlags:  []string{"factory"},
			Validation: "ignore",
		},
	}

	err := image.SetupSeed(s.tsto, model, opts)
	c.Check(err, IsNil)
}
