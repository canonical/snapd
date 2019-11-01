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

package seed_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type seed20Suite struct {
	testutil.BaseTest

	*seedtest.TestingSeed20
	devAcct *asserts.Account

	db *asserts.Database

	perfTimings timings.Measurer
}

var _ = Suite(&seed20Suite{})

func (s *seed20Suite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	// needed by TestingSeed20.MakeSeed (to work with makeSnap)

	s.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")
	assertstest.AddMany(s.StoreSigning, s.devAcct)

	s.SeedDir = c.MkDir()

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db

	s.perfTimings = timings.New(nil)
}

func (s *seed20Suite) commitTo(b *asserts.Batch) error {
	return b.CommitTo(s.db, nil)
}

func (s *seed20Suite) makeSnap(c *C, yamlKey, publisher string) {
	if publisher == "" {
		publisher = "canonical"
	}
	s.MakeAssertedSnap(c, snapYaml[yamlKey], nil, snap.R(1), publisher, s.StoreSigning.Database)
}

func (s *seed20Suite) expectedPath(snapName string) string {
	return filepath.Join(s.SeedDir, "snaps", filepath.Base(s.AssertedSnapInfo(snapName).MountFile()))
}

func (s *seed20Suite) TestLoadMetaCore20Minimal(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20101018"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
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
			}},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:      s.expectedPath("snapd"),
			SideInfo:  &s.AssertedSnapInfo("snapd").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "latest/stable",
		}, {
			Path:      s.expectedPath("pc-kernel"),
			SideInfo:  &s.AssertedSnapInfo("pc-kernel").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "20",
		}, {
			Path:      s.expectedPath("core20"),
			SideInfo:  &s.AssertedSnapInfo("core20").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "latest/stable",
		}, {
			Path:      s.expectedPath("pc"),
			SideInfo:  &s.AssertedSnapInfo("pc").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.AssertedSnapInfo("required20").Contact = "author@example.com"

	sysLabel := "20101018"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
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
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:      s.expectedPath("snapd"),
			SideInfo:  &s.AssertedSnapInfo("snapd").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "latest/stable",
		}, {
			Path:      s.expectedPath("pc-kernel"),
			SideInfo:  &s.AssertedSnapInfo("pc-kernel").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "20",
		}, {
			Path:      s.expectedPath("core20"),
			SideInfo:  &s.AssertedSnapInfo("core20").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "latest/stable",
		}, {
			Path:      s.expectedPath("pc"),
			SideInfo:  &s.AssertedSnapInfo("pc").SideInfo,
			Essential: true,
			Required:  true,
			Channel:   "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required20"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		},
	})
}
