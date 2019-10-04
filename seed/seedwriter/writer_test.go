// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package seedwriter_test

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type writerSuite struct {
	testutil.BaseTest

	// SeedSnaps helps creating and making available seed snaps
	// (it provides MakeAssertedSnap etc.) for the tests.
	*seedtest.SeedSnaps

	opts *seedwriter.Options

	db         *asserts.Database
	newFetcher seedwriter.NewFetcherFunc
	rf         seedwriter.RefAssertsFetcher

	devAcct *asserts.Account

	snapRevs map[string]*asserts.SnapRevision
	aRefs    map[string][]*asserts.Ref
}

var _ = Suite(&writerSuite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *writerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	dir := c.MkDir()
	seedDir := filepath.Join(dir, "seed")
	err := os.Mkdir(seedDir, 0755)
	c.Assert(err, IsNil)

	s.opts = &seedwriter.Options{
		RootDir: "not-root",
		SeedDir: seedDir,
	}

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical", s)
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)

	s.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")
	assertstest.AddMany(s.StoreSigning, s.devAcct)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.StoreSigning.Find)
	}
	s.newFetcher = func(save func(asserts.Assertion) error) asserts.Fetcher {
		save2 := func(a asserts.Assertion) error {
			// for checking
			err := db.Add(a)
			if err != nil {
				if _, ok := err.(*asserts.RevisionError); ok {
					return nil
				}
				return err
			}
			return save(a)
		}
		return asserts.NewFetcher(db, retrieve, save2)
	}
	s.rf = seedwriter.MakeRefAssertsFetcher(s.newFetcher)

	s.snapRevs = make(map[string]*asserts.SnapRevision)
	s.aRefs = make(map[string][]*asserts.Ref)
}

// TODO: share this with seed over seedtest as Sample* ?
var snapYaml = map[string]string{
	"core": `name: core
type: os
version: 1.0
`,
	"pc-kernel": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc": `name: pc
type: gadget
version: 1.0
`,
	"required": `name: required
type: app
version: 1.0
`,
	"classic-snap": `name: classic-snap
type: app
confinement: classic
version: 1.0
`,
	"snapd": `name: snapd
type: snapd
version: 1.0
`,
	"core18": `name: core18
type: base
version: 1.0
`,
	"pc-kernel=18": `name: pc-kernel
type: kernel
version: 1.0
`,
	"pc=18": `name: pc
type: gadget
base: core18
version: 1.0
`,
	"cont-producer": `name: cont-producer
type: app
base: core18
version: 1.1
slots:
   cont:
     interface: content
     content: cont
`,
	"cont-consumer": `name: cont-consumer
base: core18
version: 1.0
plugs:
   cont:
     interface: content
     content: cont
     default-provider: cont-producer
`,
	"classic-gadget": `name: classic-gadget
version: 1.0
type: gadget
`,
	"classic-gadget18": `name: classic-gadget18
version: 1.0
base: core18
type: gadget
`,
	"required18": `name: required18
type: app
base: core18
version: 1.0
`,
	"required-base-core16": `name: required-base-core16
type: app
base: core16
version: 1.0
`,
}

func (s *writerSuite) makeSnap(c *C, yamlKey, publisher string) {
	if publisher == "" {
		publisher = "canonical"
	}
	decl, rev := s.MakeAssertedSnap(c, snapYaml[yamlKey], nil, snap.R(1), publisher)
	assertstest.AddMany(s.StoreSigning, decl, rev)
	s.snapRevs[decl.SnapName()] = rev
}

func (s *writerSuite) makeLocalSnap(c *C, yamlKey string) (fname string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYaml[yamlKey], nil)
}

func (s *writerSuite) doFillMetaDownloadedSnap(c *C, w *seedwriter.Writer, sn *seedwriter.SeedSnap) *snap.Info {
	info := s.AssertedSnapInfo(sn.SnapName())
	err := w.SetInfo(sn, info)
	c.Assert(err, IsNil)

	aRefs := s.aRefs[sn.SnapName()]
	if aRefs == nil {
		prev := len(s.rf.Refs())
		err = s.rf.Fetch(s.snapRevs[sn.SnapName()].Ref())
		c.Assert(err, IsNil)
		aRefs = s.rf.Refs()[prev:]
		s.aRefs[sn.SnapName()] = aRefs
	}
	sn.ARefs = aRefs

	return info
}

func (s *writerSuite) fillDownloadedSnap(c *C, w *seedwriter.Writer, sn *seedwriter.SeedSnap) {
	info := s.doFillMetaDownloadedSnap(c, w, sn)

	c.Assert(sn.Path, Equals, filepath.Join(s.opts.SeedDir, "snaps", filepath.Base(info.MountFile())))
	err := os.Rename(s.AssertedSnap(sn.SnapName()), sn.Path)
	c.Assert(err, IsNil)
}

func (s *writerSuite) fillMetaDownloadedSnap(c *C, w *seedwriter.Writer, sn *seedwriter.SeedSnap) {
	s.doFillMetaDownloadedSnap(c, w, sn)
}

func (s *writerSuite) TestNewDefaultChannelError(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	s.opts.DefaultChannel = "foo/bar"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(w, IsNil)
	c.Check(err, ErrorMatches, `cannot use global default option channel: invalid risk in channel name: foo/bar`)
}

func (s writerSuite) TestSetOptionsSnapsErrors(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	tests := []struct {
		snaps []*seedwriter.OptionsSnap
		err   string
	}{
		{[]*seedwriter.OptionsSnap{{Name: "foo%&"}}, `invalid snap name: "foo%&"`},
		{[]*seedwriter.OptionsSnap{{Name: "foo_1"}}, `cannot use snap "foo_1", parallel snap instances are unsupported`},
		{[]*seedwriter.OptionsSnap{{Name: "foo"}, {Name: "foo"}}, `snap "foo" is repeated in options`},
		{[]*seedwriter.OptionsSnap{{Name: "foo", Channel: "track/foo"}}, `cannot use option channel for snap "foo": invalid risk in channel name: track/foo`},
		{[]*seedwriter.OptionsSnap{{Path: "not-a-snap"}}, `local option snap "not-a-snap" does not end in .snap`},
		{[]*seedwriter.OptionsSnap{{Path: "not-there.snap"}}, `local option snap "not-there.snap" does not exist`},
		{[]*seedwriter.OptionsSnap{{Name: "foo", Path: "foo.snap"}}, `cannot specify both name and path for option snap "foo"`},
	}

	for _, t := range tests {
		w, err := seedwriter.New(model, s.opts)
		c.Assert(err, IsNil)

		c.Check(w.SetOptionsSnaps(t.snaps), ErrorMatches, t.err)
	}
}

func (s *writerSuite) TestSnapsToDownloadCore16(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 4)

	c.Check(naming.SameSnap(snaps[0], naming.Snap("core")), Equals, true)
	c.Check(naming.SameSnap(snaps[1], naming.Snap("pc-kernel")), Equals, true)
	c.Check(snaps[1].Channel, Equals, "stable")
	c.Check(naming.SameSnap(snaps[2], naming.Snap("pc")), Equals, true)
	c.Check(snaps[2].Channel, Equals, "edge")
	c.Check(naming.SameSnap(snaps[3], naming.Snap("required")), Equals, true)
}

func (s *writerSuite) TestDownloadedCore16(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "")
	s.makeSnap(c, "pc", "")
	s.makeSnap(c, "required", "developerid")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 4)

	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)
}

func (s *writerSuite) TestDownloadedCore18(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 6)
	c.Check(naming.SameSnap(snaps[0], naming.Snap("snapd")), Equals, true)
	c.Check(naming.SameSnap(snaps[1], naming.Snap("pc-kernel")), Equals, true)
	c.Check(naming.SameSnap(snaps[2], naming.Snap("core18")), Equals, true)
	c.Check(naming.SameSnap(snaps[3], naming.Snap("pc")), Equals, true)
	c.Check(snaps[3].Channel, Equals, "18/edge")

	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)
}

func (s *writerSuite) TestSnapsToDownloadCore18IncompatibleTrack(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc-kernel", Channel: "18.1"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	_, err = w.SnapsToDownload()
	c.Check(err, ErrorMatches, `option channel "18.1" for kernel "pc-kernel" has a track incompatible with the track from model assertion: 18`)
}

func (s *writerSuite) TestSnapsToDownloadDefaultChannel(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	s.opts.DefaultChannel = "candidate"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 6)

	for i, name := range []string{"snapd", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer"} {
		c.Check(naming.SameSnap(snaps[i], naming.Snap(name)), Equals, true)
		channel := "candidate"
		switch name {
		case "pc-kernel":
			channel = "18/candidate"
		case "pc":
			channel = "18/edge"
		}
		c.Check(snaps[i].Channel, Equals, channel)
	}
}

func (s *writerSuite) upToDownloaded(c *C, model *asserts.Model, fill func(c *C, w *seedwriter.Writer, sn *seedwriter.SeedSnap)) (complete bool, w *seedwriter.Writer, err error) {
	w, err = seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)

	for _, sn := range snaps {
		fill(c, w, sn)
	}

	complete, err = w.Downloaded()
	return complete, w, err
}

func (s *writerSuite) TestDownloadedCheckBaseGadget(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc",
		"kernel":       "pc-kernel=18",
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc", "")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use gadget snap because its base "" is different from model base "core18"`)

}

func (s *writerSuite) TestDownloadedCheckBase(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"cont-producer"},
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "")
	s.makeSnap(c, "pc", "")
	s.makeSnap(c, "cont-producer", "developerid")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot add snap "cont-producer" without also adding its base "core18" explicitly`)

}

func (s *writerSuite) TestOutOfOrder(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	c.Check(w.WriteMeta(), ErrorMatches, "internal error: seedwriter.Writer expected Start|SetOptionsSnaps to be invoked on it at this point, not WriteMeta")
	c.Check(w.SeedSnaps(nil), ErrorMatches, "internal error: seedwriter.Writer expected Start|SetOptionsSnaps to be invoked on it at this point, not SeedSnaps")

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)
	_, err = w.Downloaded()
	c.Check(err, ErrorMatches, "internal error: seedwriter.Writer expected SnapToDownload|LocalSnaps to be invoked on it at this point, not Downloaded")
}

func (s *writerSuite) TestOutOfOrderWithLocalSnaps(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	requiredFn := s.makeLocalSnap(c, "required")

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Path: requiredFn}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	_, err = w.SnapsToDownload()
	c.Check(err, ErrorMatches, `internal error: seedwriter.Writer expected LocalSnaps to be invoked on it at this point, not SnapsToDownload`)

	_, err = w.LocalSnaps()
	c.Assert(err, IsNil)

	_, err = w.SnapsToDownload()
	c.Check(err, ErrorMatches, `internal error: seedwriter.Writer expected InfoDerived to be invoked on it at this point, not SnapsToDownload`)
}

func (s *writerSuite) TestDownloadedInfosNotSet(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	doNothingFill := func(*C, *seedwriter.Writer, *seedwriter.SeedSnap) {}

	_, _, err := s.upToDownloaded(c, model, doNothingFill)
	c.Check(err, ErrorMatches, `internal error: before seedwriter.Writer.Downloaded snap \"core\" Info should have been set`)
}

func (s *writerSuite) TestDownloadedUnexpectedClassicSnap(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"classic-snap"},
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "")
	s.makeSnap(c, "pc", "")
	s.makeSnap(c, "classic-snap", "developerid")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use classic snap "classic-snap" in a core system`)
}

func (s *writerSuite) TestDownloadedPublisherMismatchKernel(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "developerid")
	s.makeSnap(c, "pc", "")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use kernel "pc-kernel" published by "developerid" for model by "my-brand"`)
}

func (s *writerSuite) TestDownloadedPublisherMismatchGadget(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "")
	s.makeSnap(c, "pc", "developerid")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use gadget "pc" published by "developerid" for model by "my-brand"`)
}

func (s *writerSuite) TestDownloadedMissingDefaultProvider(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-consumer", "developerid")

	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use snap "cont-consumer" without its default content provider "cont-producer" being added explicitly`)
}

func (s *writerSuite) TestDownloadedCheckType(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	core18headers := map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	}

	tests := []struct {
		header        string
		wrongTypeSnap string
		what          string
	}{
		{"base", "core", "boot base"},
		{"gadget", "cont-consumer", "gadget"},
		{"kernel", "cont-consumer", "kernel"},
		{"required-snaps", "pc", "snap"},
		{"required-snaps", "pc-kernel", "snap"},
	}

	for _, t := range tests {
		var wrongTypeSnap interface{} = t.wrongTypeSnap
		if t.header == "required-snaps" {
			wrongTypeSnap = []interface{}{wrongTypeSnap}
		}
		model := s.Brands.Model("my-brand", "my-model", core18headers, map[string]interface{}{
			t.header: wrongTypeSnap,
		})

		_, _, err := s.upToDownloaded(c, model, s.fillMetaDownloadedSnap)

		expErr := fmt.Sprintf("%s %q has unexpected type: %v", t.what, t.wrongTypeSnap, s.AssertedSnapInfo(t.wrongTypeSnap).GetType())
		c.Check(err, ErrorMatches, expErr)
	}
}

func (s *writerSuite) TestDownloadedCheckTypeSnapd(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")

	// break type
	s.AssertedSnapInfo("snapd").SnapType = snap.TypeGadget
	_, _, err := s.upToDownloaded(c, model, s.fillMetaDownloadedSnap)
	c.Check(err, ErrorMatches, `snapd snap has unexpected type: gadget`)
}

func (s *writerSuite) TestDownloadedCheckTypeCore(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "pc-kernel", "")
	s.makeSnap(c, "pc", "")

	// break type
	s.AssertedSnapInfo("core").SnapType = snap.TypeBase
	_, _, err := s.upToDownloaded(c, model, s.fillMetaDownloadedSnap)
	c.Check(err, ErrorMatches, `core snap has unexpected type: base`)
}

func readAssertions(c *C, fn string) []asserts.Assertion {
	f, err := os.Open(fn)
	c.Assert(err, IsNil)

	var as []asserts.Assertion
	dec := asserts.NewDecoder(f)
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, IsNil)
		as = append(as, a)
	}

	return as
}

func (s *writerSuite) TestSeedSnapsWriteMetaCore18(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 6)

	s.AssertedSnapInfo("cont-producer").Contact = "author@cont-producer.net"
	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seed.ReadYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 6)

	// check the files are in place
	for i, name := range []string{"snapd", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		channel := "stable"
		switch name {
		case "pc-kernel":
			channel = "18"
		case "pc":
			channel = "18/edge"
		}

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap16{
			Name:    info.SnapName(),
			SnapID:  info.SnapID,
			Channel: channel,
			File:    fn,
			Contact: info.Contact,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 6)

	// check assertions
	seedAssertsDir := filepath.Join(s.opts.SeedDir, "assertions")
	storeAccountKeyPK := s.StoreSigning.StoreAccountKey("").PublicKeyID()
	brandAcctKeyPK := s.Brands.AccountKey("my-brand").PublicKeyID()

	for _, fn := range []string{"model", brandAcctKeyPK + ".account-key", "my-brand.account", storeAccountKeyPK + ".account-key"} {
		p := filepath.Join(seedAssertsDir, fn)
		c.Check(p, testutil.FilePresent)
	}

	c.Check(filepath.Join(seedAssertsDir, "model"), testutil.FileEquals, asserts.Encode(model))

	acct := readAssertions(c, filepath.Join(seedAssertsDir, "my-brand.account"))
	c.Assert(acct, HasLen, 1)
	c.Check(acct[0].Type(), Equals, asserts.AccountType)
	c.Check(acct[0].HeaderString("account-id"), Equals, "my-brand")

	// check the snap assertions are also in place
	for _, snapName := range []string{"snapd", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer"} {
		p := filepath.Join(seedAssertsDir, fmt.Sprintf("16,%s.snap-declaration", s.AssertedSnapID(snapName)))
		decl := readAssertions(c, p)
		c.Assert(decl, HasLen, 1)
		c.Check(decl[0].Type(), Equals, asserts.SnapDeclarationType)
		c.Check(decl[0].HeaderString("snap-name"), Equals, snapName)
		p = filepath.Join(seedAssertsDir, fmt.Sprintf("%s.snap-revision", s.snapRevs[snapName].SnapSHA3_384()))
		rev := readAssertions(c, p)
		c.Assert(rev, HasLen, 1)
		c.Check(rev[0].Type(), Equals, asserts.SnapRevisionType)
		c.Check(rev[0].HeaderString("snap-id"), Equals, s.AssertedSnapID(snapName))
	}
}

func (s *writerSuite) TestSeedSnapsWriteMetaCore18StoreAssertion(c *C) {
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
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core18",
		"gadget":       "pc=18",
		"kernel":       "pc-kernel=18",
		"store":        "my-store",
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=18", "")
	s.makeSnap(c, "pc=18", "")

	complete, w, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check assertions
	seedAssertsDir := filepath.Join(s.opts.SeedDir, "assertions")
	// check the store assertion was fetched
	p := filepath.Join(seedAssertsDir, "my-store.store")
	c.Check(p, testutil.FilePresent)
}

func (s *writerSuite) TestLocalSnaps(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	core18Fn := s.makeLocalSnap(c, "core18")
	pcKernelFn := s.makeLocalSnap(c, "pc-kernel=18")
	pcFn := s.makeLocalSnap(c, "pc=18")
	contConsumerFn := s.makeLocalSnap(c, "cont-consumer")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{
		{Path: core18Fn},
		{Path: pcFn, Channel: "edge"},
		{Path: pcKernelFn},
		{Path: contConsumerFn},
	})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Assert(localSnaps, HasLen, 4)
	c.Check(localSnaps[0].Path, Equals, core18Fn)
	c.Check(localSnaps[1].Path, Equals, pcFn)
	c.Check(localSnaps[2].Path, Equals, pcKernelFn)
	c.Check(localSnaps[3].Path, Equals, contConsumerFn)
}

func (s *writerSuite) TestLocalSnapsFullUse(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "cont-producer", "developerid")

	core18Fn := s.makeLocalSnap(c, "core18")
	pcKernelFn := s.makeLocalSnap(c, "pc-kernel=18")
	pcFn := s.makeLocalSnap(c, "pc=18")
	contConsumerFn := s.makeLocalSnap(c, "cont-consumer")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{
		{Path: core18Fn},
		{Name: "pc-kernel", Channel: "candidate"},
		{Path: pcFn, Channel: "edge"},
		{Path: pcKernelFn},
		{Path: s.AssertedSnap("cont-producer")},
		{Path: contConsumerFn},
	})
	c.Assert(err, IsNil)

	tf, err := w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Assert(localSnaps, HasLen, 5)

	for _, sn := range localSnaps {
		si, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, tf, s.db)
		if !asserts.IsNotFound(err) {
			c.Assert(err, IsNil)
		}
		f, err := snap.Open(sn.Path)
		c.Assert(err, IsNil)
		info, err := snap.ReadInfoFromSnapFile(f, si)
		c.Assert(err, IsNil)
		w.SetInfo(sn, info)
		sn.ARefs = aRefs
	}

	err = w.InfoDerived()
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 1)
	c.Check(naming.SameSnap(snaps[0], naming.Snap("snapd")), Equals, true)

	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)

	copySnap := func(name, src, dst string) error {
		return osutil.CopyFile(src, dst, 0)
	}

	err = w.SeedSnaps(copySnap)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seed.ReadYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 6)

	assertedNum := 0
	// check the files are in place
	for i, name := range []string{"snapd", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer"} {
		info := s.AssertedSnapInfo(name)
		unasserted := false
		if info == nil {
			info = &snap.Info{
				SuggestedName: name,
			}
			info.Revision = snap.R(-1)
			unasserted = true
		} else {
			assertedNum++
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		channel := "stable"
		switch name {
		case "pc-kernel":
			channel = "18/candidate"
		case "pc":
			channel = "18/edge"
		}

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap16{
			Name:       info.SnapName(),
			SnapID:     info.SnapID,
			Channel:    channel,
			File:       fn,
			Unasserted: unasserted,
		})
	}
	c.Check(assertedNum, Equals, 2)

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 6)

	// check the snap assertions are in place
	seedAssertsDir := filepath.Join(s.opts.SeedDir, "assertions")
	for _, snapName := range []string{"snapd", "cont-producer"} {
		p := filepath.Join(seedAssertsDir, fmt.Sprintf("16,%s.snap-declaration", s.AssertedSnapID(snapName)))
		decl := readAssertions(c, p)
		c.Assert(decl, HasLen, 1)
		c.Check(decl[0].Type(), Equals, asserts.SnapDeclarationType)
		c.Check(decl[0].HeaderString("snap-name"), Equals, snapName)
		p = filepath.Join(seedAssertsDir, fmt.Sprintf("%s.snap-revision", s.snapRevs[snapName].SnapSHA3_384()))
		rev := readAssertions(c, p)
		c.Assert(rev, HasLen, 1)
		c.Check(rev[0].Type(), Equals, asserts.SnapRevisionType)
		c.Check(rev[0].HeaderString("snap-id"), Equals, s.AssertedSnapID(snapName))
	}
}

func (s *writerSuite) TestInfoDerivedInfosNotSet(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	core18Fn := s.makeLocalSnap(c, "core18")
	pcKernelFn := s.makeLocalSnap(c, "pc-kernel=18")
	pcFn := s.makeLocalSnap(c, "pc=18")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{
		{Path: core18Fn},
		{Path: pcFn, Channel: "edge"},
		{Path: pcKernelFn},
	})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	_, err = w.LocalSnaps()
	c.Assert(err, IsNil)

	err = w.InfoDerived()
	c.Assert(err, ErrorMatches, `internal error: before seedwriter.Writer.InfoDerived snap ".*/core18.*.snap" Info should have been set`)
}

func (s *writerSuite) TestInfoDerivedRepeatedLocalSnap(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	core18Fn := s.makeLocalSnap(c, "core18")
	pcKernelFn := s.makeLocalSnap(c, "pc-kernel=18")
	pcFn := s.makeLocalSnap(c, "pc=18")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{
		{Path: core18Fn},
		{Path: pcFn, Channel: "edge"},
		{Path: pcKernelFn},
		{Path: core18Fn},
	})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Check(localSnaps, HasLen, 4)

	for _, sn := range localSnaps {
		f, err := snap.Open(sn.Path)
		c.Assert(err, IsNil)
		info, err := snap.ReadInfoFromSnapFile(f, nil)
		c.Assert(err, IsNil)
		w.SetInfo(sn, info)
	}

	err = w.InfoDerived()
	c.Assert(err, ErrorMatches, `local snap "core18" is repeated in options`)
}

func (s *writerSuite) TestInfoDerivedInconsistentChannel(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"base":           "core18",
		"gadget":         "pc=18",
		"kernel":         "pc-kernel=18",
		"required-snaps": []interface{}{"cont-consumer", "cont-producer"},
	})

	core18Fn := s.makeLocalSnap(c, "core18")
	pcKernelFn := s.makeLocalSnap(c, "pc-kernel=18")
	pcFn := s.makeLocalSnap(c, "pc=18")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{
		{Path: core18Fn},
		{Path: pcFn, Channel: "edge"},
		{Path: pcKernelFn},
		{Name: "pc", Channel: "beta"},
	})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Check(localSnaps, HasLen, 3)

	for _, sn := range localSnaps {
		f, err := snap.Open(sn.Path)
		c.Assert(err, IsNil)
		info, err := snap.ReadInfoFromSnapFile(f, nil)
		c.Assert(err, IsNil)
		w.SetInfo(sn, info)
	}

	err = w.InfoDerived()
	c.Assert(err, ErrorMatches, `option snap has different channels specified: ".*/pc.*.snap"="edge" vs "pc"="beta"`)
}

func (s *writerSuite) TestSeedSnapsWriteMetaClassicWithCore(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget",
		"required-snaps": []interface{}{"required"},
	})

	s.makeSnap(c, "core", "")
	s.makeSnap(c, "classic-gadget", "")
	s.makeSnap(c, "required", "developerid")

	complete, w, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Assert(err, IsNil)
	c.Check(complete, Equals, false)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)

	s.fillDownloadedSnap(c, w, snaps[0])

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seed.ReadYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)

	// check the files are in place
	for i, name := range []string{"core", "classic-gadget", "required"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap16{
			Name:    info.SnapName(),
			SnapID:  info.SnapID,
			Channel: "stable",
			File:    fn,
			Contact: info.Contact,
		})
	}
}

func (s *writerSuite) TestSeedSnapsWriteMetaClassicSnapdOnly(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget18",
		"required-snaps": []interface{}{"core18", "required18"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "classic-gadget18", "")
	s.makeSnap(c, "required18", "developerid")

	complete, w, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Assert(err, IsNil)
	c.Check(complete, Equals, false)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)

	s.fillDownloadedSnap(c, w, snaps[0])

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Check(complete, Equals, true)

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seed.ReadYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)
	c.Assert(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"snapd", "core18", "classic-gadget18", "required18"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seed.Snap16{
			Name:    info.SnapName(),
			SnapID:  info.SnapID,
			Channel: "stable",
			File:    fn,
			Contact: info.Contact,
		})
	}
}

func (s *writerSuite) TestSeedSnapsWriteMetaClassicSnapdOnlyMissingCore16(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"architecture":   "amd64",
		"gadget":         "classic-gadget18",
		"required-snaps": []interface{}{"core18", "required-base-core16"},
	})

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "classic-gadget18", "")
	s.makeSnap(c, "required-base-core16", "developerid")

	_, _, err := s.upToDownloaded(c, model, s.fillMetaDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot use "required-base-core16" requiring base "core16" without adding "core16" \(or "core"\) explicitly`)
}
