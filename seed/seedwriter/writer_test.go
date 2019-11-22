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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
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

	aRefs map[string][]*asserts.Ref
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
		SeedDir: seedDir,
	}

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical")
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

	s.aRefs = make(map[string][]*asserts.Ref)
}

var snapYaml = seedtest.MergeSampleSnapYaml(seedtest.SampleSnapYaml, map[string]string{
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
	"required-base-core16": `name: required-base-core16
type: app
base: core16
version: 1.0
`,
})

const pcGadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

var snapFiles = map[string][][]string{
	"pc": {
		{"meta/gadget.yaml", pcGadgetYaml},
	},
	"pc=18": {
		{"meta/gadget.yaml", pcGadgetYaml},
	},
}

func (s *writerSuite) makeSnap(c *C, yamlKey, publisher string) {
	if publisher == "" {
		publisher = "canonical"
	}
	s.MakeAssertedSnap(c, snapYaml[yamlKey], snapFiles[yamlKey], snap.R(1), publisher, s.StoreSigning.Database)
}

func (s *writerSuite) makeLocalSnap(c *C, yamlKey string) (fname string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYaml[yamlKey], nil)
}

func (s *writerSuite) doFillMetaDownloadedSnap(c *C, w *seedwriter.Writer, sn *seedwriter.SeedSnap) *snap.Info {
	info := s.AssertedSnapInfo(sn.SnapName())
	c.Assert(info, NotNil, Commentf("%s not defined", sn.SnapName()))
	err := w.SetInfo(sn, info)
	c.Assert(err, IsNil)

	aRefs := s.aRefs[sn.SnapName()]
	if aRefs == nil {
		prev := len(s.rf.Refs())
		err = s.rf.Fetch(s.AssertedSnapRevision(sn.SnapName()).Ref())
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

func (s *writerSuite) TestSnapsToDownloadOptionTrack(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name":   "my model",
		"architecture":   "amd64",
		"gadget":         "pc",
		"kernel":         "pc-kernel",
		"required-snaps": []interface{}{"required"},
	})

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "track/edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 4)

	c.Check(naming.SameSnap(snaps[2], naming.Snap("pc")), Equals, true)
	c.Check(snaps[2].Channel, Equals, "track/edge")
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

	essSnaps, err := w.BootSnaps()
	c.Assert(err, IsNil)
	c.Check(essSnaps, DeepEquals, snaps[:3])
	c.Check(naming.SameSnap(essSnaps[2], naming.Snap("pc")), Equals, true)
	c.Check(essSnaps[2].Channel, Equals, "edge")
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

	essSnaps, err := w.BootSnaps()
	c.Assert(err, IsNil)
	c.Check(essSnaps, DeepEquals, snaps[:4])

	c.Check(w.Warnings(), HasLen, 0)
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
	c.Check(err, ErrorMatches, `option channel "18.1" for kernel "pc-kernel" has a track incompatible with the pinned track from model assertion: 18`)
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

	_, err = w.BootSnaps()
	c.Check(err, ErrorMatches, "internal error: seedwriter.Writer cannot query seed snaps before Downloaded signaled complete")

	_, err = w.UnassertedSnaps()
	c.Check(err, ErrorMatches, "internal error: seedwriter.Writer cannot query seed snaps before Downloaded signaled complete")

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
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
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

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
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

	acct := seedtest.ReadAssertions(c, filepath.Join(seedAssertsDir, "my-brand.account"))
	c.Assert(acct, HasLen, 1)
	c.Check(acct[0].Type(), Equals, asserts.AccountType)
	c.Check(acct[0].HeaderString("account-id"), Equals, "my-brand")

	// check the snap assertions are also in place
	for _, snapName := range []string{"snapd", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer"} {
		p := filepath.Join(seedAssertsDir, fmt.Sprintf("16,%s.snap-declaration", s.AssertedSnapID(snapName)))
		decl := seedtest.ReadAssertions(c, p)
		c.Assert(decl, HasLen, 1)
		c.Check(decl[0].Type(), Equals, asserts.SnapDeclarationType)
		c.Check(decl[0].HeaderString("snap-name"), Equals, snapName)
		p = filepath.Join(seedAssertsDir, fmt.Sprintf("%s.snap-revision", s.AssertedSnapRevision(snapName).SnapSHA3_384()))
		rev := seedtest.ReadAssertions(c, p)
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

func (s *writerSuite) TestLocalSnapsCore18FullUse(c *C) {
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
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
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

		channel := ""
		if !unasserted {
			channel = "stable"
		}

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
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
		decl := seedtest.ReadAssertions(c, p)
		c.Assert(decl, HasLen, 1)
		c.Check(decl[0].Type(), Equals, asserts.SnapDeclarationType)
		c.Check(decl[0].HeaderString("snap-name"), Equals, snapName)
		p = filepath.Join(seedAssertsDir, fmt.Sprintf("%s.snap-revision", s.AssertedSnapRevision(snapName).SnapSHA3_384()))
		rev := seedtest.ReadAssertions(c, p)
		c.Assert(rev, HasLen, 1)
		c.Check(rev[0].Type(), Equals, asserts.SnapRevisionType)
		c.Check(rev[0].HeaderString("snap-id"), Equals, s.AssertedSnapID(snapName))
	}

	unassertedSnaps, err := w.UnassertedSnaps()
	c.Assert(err, IsNil)
	c.Check(unassertedSnaps, HasLen, 4)
	unassertedSet := naming.NewSnapSet(unassertedSnaps)
	for _, snapName := range []string{"core18", "pc-kernel", "pc", "cont-consumer"} {
		c.Check(unassertedSet.Contains(naming.Snap(snapName)), Equals, true)
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

	_, err = w.BootSnaps()
	c.Check(err, ErrorMatches, "no snaps participating in boot on classic")

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)

	c.Check(seedYaml.Snaps, HasLen, 3)

	// check the files are in place
	for i, name := range []string{"core", "classic-gadget", "required"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
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
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)
	c.Assert(seedYaml.Snaps, HasLen, 4)

	// check the files are in place
	for i, name := range []string{"snapd", "core18", "classic-gadget18", "required18"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
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

func (s *writerSuite) TestSeedSnapsWriteMetaExtraSnaps(c *C) {
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
	s.makeSnap(c, "core", "")
	s.makeSnap(c, "required", "developerid")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "required", Channel: "beta"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 6)

	s.AssertedSnapInfo("cont-producer").Contact = "author@cont-producer.net"
	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, false)

	snaps, err = w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Check(naming.SameSnap(snaps[0], naming.Snap("required")), Equals, true)

	s.fillDownloadedSnap(c, w, snaps[0])

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, false)

	snaps, err = w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Check(naming.SameSnap(snaps[0], naming.Snap("core")), Equals, true)

	s.fillDownloadedSnap(c, w, snaps[0])

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, true)

	err = w.SeedSnaps(nil)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)
	c.Assert(seedYaml.Snaps, HasLen, 8)

	// check the files are in place
	for i, name := range []string{"snapd", "core", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer", "required"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		channel := "stable"
		switch name {
		case "pc-kernel", "pc":
			channel = "18"
		case "required":
			channel = "beta"
		}

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
			Name:    info.SnapName(),
			SnapID:  info.SnapID,
			Channel: channel,
			File:    fn,
			Contact: info.Contact,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 8)

	// check the snap assertions are also in place
	seedAssertsDir := filepath.Join(s.opts.SeedDir, "assertions")
	for _, snapName := range []string{"snapd", "core", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer", "required"} {
		p := filepath.Join(seedAssertsDir, fmt.Sprintf("16,%s.snap-declaration", s.AssertedSnapID(snapName)))
		decl := seedtest.ReadAssertions(c, p)
		c.Assert(decl, HasLen, 1)
		c.Check(decl[0].Type(), Equals, asserts.SnapDeclarationType)
		c.Check(decl[0].HeaderString("snap-name"), Equals, snapName)
		p = filepath.Join(seedAssertsDir, fmt.Sprintf("%s.snap-revision", s.AssertedSnapRevision(snapName).SnapSHA3_384()))
		rev := seedtest.ReadAssertions(c, p)
		c.Assert(rev, HasLen, 1)
		c.Check(rev[0].Type(), Equals, asserts.SnapRevisionType)
		c.Check(rev[0].HeaderString("snap-id"), Equals, s.AssertedSnapID(snapName))
	}

	c.Check(w.Warnings(), DeepEquals, []string{
		`model has base "core18" but some snaps ("required") require "core" as base as well, for compatibility it was added implicitly, adding "core" explicitly is recommended`,
	})
}

func (s *writerSuite) TestSeedSnapsWriteMetaLocalExtraSnaps(c *C) {
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
	s.makeSnap(c, "core", "")
	requiredFn := s.makeLocalSnap(c, "required")

	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Path: requiredFn}})
	c.Assert(err, IsNil)

	tf, err := w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Assert(localSnaps, HasLen, 1)

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
	c.Assert(snaps, HasLen, 6)

	s.AssertedSnapInfo("cont-producer").Contact = "author@cont-producer.net"
	for _, sn := range snaps {
		s.fillDownloadedSnap(c, w, sn)
	}

	complete, err := w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, false)

	snaps, err = w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 0)

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, false)

	snaps, err = w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 1)
	c.Check(naming.SameSnap(snaps[0], naming.Snap("core")), Equals, true)

	s.fillDownloadedSnap(c, w, snaps[0])

	complete, err = w.Downloaded()
	c.Assert(err, IsNil)
	c.Assert(complete, Equals, true)

	copySnap := func(name, src, dst string) error {
		return osutil.CopyFile(src, dst, 0)
	}

	err = w.SeedSnaps(copySnap)
	c.Assert(err, IsNil)

	err = w.WriteMeta()
	c.Assert(err, IsNil)

	// check seed
	seedYaml, err := seedwriter.InternalReadSeedYaml(filepath.Join(s.opts.SeedDir, "seed.yaml"))
	c.Assert(err, IsNil)
	c.Assert(seedYaml.Snaps, HasLen, 8)

	// check the files are in place
	for i, name := range []string{"snapd", "core", "pc-kernel", "core18", "pc", "cont-consumer", "cont-producer", "required"} {
		info := s.AssertedSnapInfo(name)
		unasserted := false
		if info == nil {
			info = &snap.Info{
				SuggestedName: name,
			}
			info.Revision = snap.R(-1)
			unasserted = true
		}

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(osutil.FileExists(p), Equals, true)

		channel := ""
		if !unasserted {
			switch name {
			case "pc-kernel", "pc":
				channel = "18"
			default:
				channel = "stable"
			}
		}

		c.Check(seedYaml.Snaps[i], DeepEquals, &seedwriter.InternalSnap16{
			Name:       info.SnapName(),
			SnapID:     info.SnapID,
			Channel:    channel,
			File:       fn,
			Contact:    info.Contact,
			Unasserted: unasserted,
		})
	}

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 8)

	unassertedSnaps, err := w.UnassertedSnaps()
	c.Assert(err, IsNil)
	c.Check(unassertedSnaps, HasLen, 1)
	c.Check(naming.SameSnap(unassertedSnaps[0], naming.Snap("required")), Equals, true)
}

func (s *writerSuite) TestSeedSnapsWriteMetaCore20(c *C) {
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
		"store":        "my-store",
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
				"name": "core18",
				"id":   s.AssertedSnapID("core18"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "cont-consumer",
				"id":   s.AssertedSnapID("cont-consumer"),
			},
			map[string]interface{}{
				"name": "cont-producer",
				"id":   s.AssertedSnapID("cont-producer"),
			},
		},
	})

	// sanity
	c.Assert(model.Grade(), Equals, asserts.ModelSigned)

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "cont-producer", "developerid")
	s.makeSnap(c, "cont-consumer", "developerid")

	s.opts.Label = "20191003"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 7)

	s.AssertedSnapInfo("cont-producer").Contact = "author@cont-producer.net"
	s.AssertedSnapInfo("cont-consumer").Private = true
	for _, sn := range snaps {
		// check the used channel at this level because in the
		// non-dangerous case is not written anywhere (it
		// reflects the model or default)
		channel := "latest/stable"
		switch sn.SnapName() {
		case "pc", "pc-kernel":
			channel = "20"
		}
		c.Check(sn.Channel, Equals, channel)
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
	systemDir := filepath.Join(s.opts.SeedDir, "systems", s.opts.Label)
	c.Check(systemDir, testutil.FilePresent)

	// check the snaps are in place
	for _, name := range []string{"snapd", "pc-kernel", "core20", "pc", "core18", "cont-consumer", "cont-producer"} {
		info := s.AssertedSnapInfo(name)

		fn := filepath.Base(info.MountFile())
		p := filepath.Join(s.opts.SeedDir, "snaps", fn)
		c.Check(p, testutil.FilePresent)
	}

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 7)

	// check assertions
	c.Check(filepath.Join(systemDir, "model"), testutil.FileEquals, asserts.Encode(model))

	assertsDir := filepath.Join(systemDir, "assertions")
	modelEtc := seedtest.ReadAssertions(c, filepath.Join(assertsDir, "model-etc"))
	c.Check(modelEtc, HasLen, 4)

	keyPKs := make(map[string]bool)
	for _, a := range modelEtc {
		switch a.Type() {
		case asserts.AccountType:
			c.Check(a.HeaderString("account-id"), Equals, "my-brand")
		case asserts.StoreType:
			c.Check(a.HeaderString("store"), Equals, "my-store")
		case asserts.AccountKeyType:
			keyPKs[a.HeaderString("public-key-sha3-384")] = true
		default:
			c.Fatalf("unexpected assertion %s", a.Type().Name)
		}
	}
	c.Check(keyPKs, DeepEquals, map[string]bool{
		s.StoreSigning.StoreAccountKey("").PublicKeyID(): true,
		s.Brands.AccountKey("my-brand").PublicKeyID():    true,
	})

	// check snap assertions
	snapAsserts := seedtest.ReadAssertions(c, filepath.Join(assertsDir, "snaps"))
	seen := make(map[string]bool)

	for _, a := range snapAsserts {
		uniq := a.Ref().Unique()
		if a.Type() == asserts.SnapRevisionType {
			rev := a.(*asserts.SnapRevision)
			uniq = fmt.Sprintf("%s@%d", rev.SnapID(), rev.SnapRevision())
		}
		seen[uniq] = true
	}

	snapRevUniq := func(snapName string, revno int) string {
		return fmt.Sprintf("%s@%d", s.AssertedSnapID(snapName), revno)
	}
	snapDeclUniq := func(snapName string) string {
		return "snap-declaration/16/" + s.AssertedSnapID(snapName)
	}

	c.Check(seen, DeepEquals, map[string]bool{
		"account/developerid":           true,
		snapDeclUniq("snapd"):           true,
		snapDeclUniq("pc-kernel"):       true,
		snapDeclUniq("pc"):              true,
		snapDeclUniq("core20"):          true,
		snapDeclUniq("core18"):          true,
		snapDeclUniq("cont-consumer"):   true,
		snapDeclUniq("cont-producer"):   true,
		snapRevUniq("snapd", 1):         true,
		snapRevUniq("pc-kernel", 1):     true,
		snapRevUniq("pc", 1):            true,
		snapRevUniq("core20", 1):        true,
		snapRevUniq("core18", 1):        true,
		snapRevUniq("cont-consumer", 1): true,
		snapRevUniq("cont-producer", 1): true,
	})

	c.Check(filepath.Join(systemDir, "extra-snaps"), testutil.FileAbsent)

	// check auxiliary store info
	l, err = ioutil.ReadDir(filepath.Join(systemDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 1)

	b, err := ioutil.ReadFile(filepath.Join(systemDir, "snaps", "aux-info.json"))
	c.Assert(err, IsNil)
	var auxInfos map[string]map[string]interface{}
	err = json.Unmarshal(b, &auxInfos)
	c.Assert(err, IsNil)
	c.Check(auxInfos, DeepEquals, map[string]map[string]interface{}{
		s.AssertedSnapID("cont-consumer"): {
			"private": true,
		},
		s.AssertedSnapID("cont-producer"): {
			"contact": "author@cont-producer.net",
		},
	})

	c.Check(filepath.Join(systemDir, "options.yaml"), testutil.FileAbsent)
}

func (s *writerSuite) TestCore20InvalidLabel(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-store",
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
		},
	})

	invalid := []string{
		"-",
		"a.b",
		"aa--b",
	}

	for _, inv := range invalid {
		s.opts.Label = inv
		w, err := seedwriter.New(model, s.opts)
		c.Assert(w, IsNil)
		c.Check(err, ErrorMatches, `system label contains invalid characters:.*`)
	}
}

func (s *writerSuite) TestDownloadedCore20CheckBase(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-store",
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
				"name": "cont-producer",
				"id":   s.AssertedSnapID("cont-producer"),
			},
		},
	})

	// sanity
	c.Assert(model.Grade(), Equals, asserts.ModelSigned)

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "cont-producer", "developerid")

	s.opts.Label = "20191003"
	_, _, err := s.upToDownloaded(c, model, s.fillDownloadedSnap)
	c.Check(err, ErrorMatches, `cannot add snap "cont-producer" without also adding its base "core18" explicitly`)
}

func (s *writerSuite) TestDownloadedCore20CheckBaseCoreXX(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core", "")
	s.makeSnap(c, "required", "")
	s.makeSnap(c, "required-base-core16", "")

	coreEnt := map[string]interface{}{
		"name": "core",
		"id":   s.AssertedSnapID("core"),
		"type": "core",
	}
	requiredEnt := map[string]interface{}{
		"name": "required",
		"id":   s.AssertedSnapID("required"),
	}

	requiredBaseCore16Ent := map[string]interface{}{
		"name": "required-base-core16",
		"id":   s.AssertedSnapID("required-base-core16"),
	}

	tests := []struct {
		snaps []interface{}
		err   string
	}{
		{[]interface{}{coreEnt, requiredEnt}, ""},
		{[]interface{}{coreEnt, requiredBaseCore16Ent}, ""},
		{[]interface{}{requiredEnt}, `cannot add snap "required" without also adding its base "core" explicitly`},
		{[]interface{}{requiredBaseCore16Ent}, `cannot add snap "required-base-core16" without also adding its base "core16" \(or "core"\) explicitly`},
	}

	s.opts.Label = "20191003"
	for _, t := range tests {
		snaps := []interface{}{
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
		}

		snaps = append(snaps, t.snaps...)

		model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
			"display-name": "my model",
			"architecture": "amd64",
			"store":        "my-store",
			"base":         "core20",
			"snaps":        snaps,
		})

		_, _, err := s.upToDownloaded(c, model, s.fillMetaDownloadedSnap)
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *writerSuite) TestCore20NonDangerousNoChannelOverride(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-store",
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
		},
	})

	s.opts.DefaultChannel = "stable"
	s.opts.Label = "20191107"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(w, IsNil)
	c.Check(err, ErrorMatches, `cannot override channels, add local snaps or extra snaps with a model of grade higher than dangerous`)
}

func (s *writerSuite) TestCore20NonDangerousNoOptionsSnapsAllowed(c *C) {
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-store",
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
		},
	})

	s.opts.Label = "20191107"

	tests := []struct {
		optSnaps []*seedwriter.OptionsSnap
		err      bool
	}{
		{nil, false},
		{[]*seedwriter.OptionsSnap{}, false},
		{[]*seedwriter.OptionsSnap{{Name: "extra"}}, true},
		{[]*seedwriter.OptionsSnap{{Path: "local.snap"}}, true},
		{[]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}}, true},
	}

	for _, t := range tests {
		w, err := seedwriter.New(model, s.opts)
		c.Assert(err, IsNil)

		err = w.SetOptionsSnaps(t.optSnaps)
		if t.err {
			c.Check(err, ErrorMatches, `cannot override channels, add local snaps or extra snaps with a model of grade higher than dangerous`)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *writerSuite) TestSeedSnapsWriteMetaCore20LocalSnaps(c *C) {
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
		"store":        "my-store",
		"base":         "core20",
		"grade":        "dangerous",
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
			},
		},
	})

	// sanity
	c.Assert(model.Grade(), Equals, asserts.ModelDangerous)

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	s.opts.Label = "20191030"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Path: requiredFn}})
	c.Assert(err, IsNil)

	tf, err := w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	localSnaps, err := w.LocalSnaps()
	c.Assert(err, IsNil)
	c.Assert(localSnaps, HasLen, 1)

	for _, sn := range localSnaps {
		si, aRefs, err := seedwriter.DeriveSideInfo(sn.Path, tf, s.db)
		c.Check(asserts.IsNotFound(err), Equals, true)
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
	c.Check(snaps, HasLen, 4)

	for _, sn := range snaps {
		// check the used channel at this level because in the
		// non-dangerous case is not written anywhere (it
		// reflects the model or default)
		channel := "latest/stable"
		switch sn.SnapName() {
		case "pc", "pc-kernel":
			channel = "20"
		}
		c.Check(sn.Channel, Equals, channel)
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
	systemDir := filepath.Join(s.opts.SeedDir, "systems", s.opts.Label)
	c.Check(systemDir, testutil.FilePresent)

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 4)

	// local unasserted snap was put in system snaps dir
	c.Check(filepath.Join(systemDir, "snaps", "required20_1.0.snap"), testutil.FilePresent)

	options20, err := seedwriter.InternalReadOptions20(filepath.Join(systemDir, "options.yaml"))
	c.Assert(err, IsNil)

	c.Check(options20.Snaps, DeepEquals, []*seedwriter.InternalSnap20{
		{
			Name:       "required20",
			SnapID:     s.AssertedSnapID("required20"),
			Unasserted: "required20_1.0.snap",
		},
	})
}

func (s *writerSuite) TestSeedSnapsWriteMetaCore20ChannelOverrides(c *C) {
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
		"store":        "my-store",
		"base":         "core20",
		"grade":        "dangerous",
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
			},
		},
	})

	// sanity
	c.Assert(model.Grade(), Equals, asserts.ModelDangerous)

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.opts.Label = "20191030"
	s.opts.DefaultChannel = "candidate"
	w, err := seedwriter.New(model, s.opts)
	c.Assert(err, IsNil)

	err = w.SetOptionsSnaps([]*seedwriter.OptionsSnap{{Name: "pc", Channel: "edge"}})
	c.Assert(err, IsNil)

	_, err = w.Start(s.db, s.newFetcher)
	c.Assert(err, IsNil)

	snaps, err := w.SnapsToDownload()
	c.Assert(err, IsNil)
	c.Check(snaps, HasLen, 5)

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
	systemDir := filepath.Join(s.opts.SeedDir, "systems", s.opts.Label)
	c.Check(systemDir, testutil.FilePresent)

	l, err := ioutil.ReadDir(filepath.Join(s.opts.SeedDir, "snaps"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 5)

	options20, err := seedwriter.InternalReadOptions20(filepath.Join(systemDir, "options.yaml"))
	c.Assert(err, IsNil)

	c.Check(options20.Snaps, DeepEquals, []*seedwriter.InternalSnap20{
		{
			Name:    "snapd",
			Channel: "latest/candidate",
		},
		{
			Name:    "pc-kernel",
			SnapID:  s.AssertedSnapID("pc-kernel"),
			Channel: "20/candidate",
		},
		{
			Name:    "core20",
			Channel: "latest/candidate",
		},
		{
			Name:    "pc",
			SnapID:  s.AssertedSnapID("pc"),
			Channel: "20/edge",
		},
		{
			Name:    "required20",
			SnapID:  s.AssertedSnapID("required20"),
			Channel: "latest/candidate",
		},
	})
}
