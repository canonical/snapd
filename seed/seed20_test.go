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
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
	}, nil)

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

func (s *seed20Suite) makeCore20MinimalSeed(c *C, sysLabel string) string {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

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
	}, nil)

	return filepath.Join(s.SeedDir, "systems", sysLabel)
}

func (s *seed20Suite) TestLoadAssertionsModelTempDBHappy(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	sysLabel := "20191031"
	s.makeCore20MinimalSeed(c, sysLabel)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	model, err := seed20.Model()
	c.Assert(err, IsNil)
	c.Check(model.Model(), Equals, "my-model")
	c.Check(model.Base(), Equals, "core20")
}

func (s *seed20Suite) TestLoadAssertionsMultiModels(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	err := osutil.CopyFile(filepath.Join(sysDir, "model"), filepath.Join(sysDir, "assertions", "model2"), 0)
	c.Assert(err, IsNil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `system cannot have any model assertion but the one in the system model assertion file`)
}

func (s *seed20Suite) TestLoadAssertionsInvalidModelAssertFile(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	modelAssertFn := filepath.Join(sysDir, "model")

	// copy over multiple assertions
	err := osutil.CopyFile(filepath.Join(sysDir, "assertions", "model-etc"), modelAssertFn, osutil.CopyFlagOverwrite)
	c.Assert(err, IsNil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `system model assertion file must contain exactly the model assertion`)

	// write whatever single non model assertion
	seedtest.WriteAssertions(modelAssertFn, s.AssertedSnapRevision("snapd"))

	seed20, err = seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `system model assertion file must contain exactly the model assertion`)
}

func (s *seed20Suite) massageAssertions(c *C, fn string, filter func(asserts.Assertion) asserts.Assertion) {
	assertions := seedtest.ReadAssertions(c, fn)
	filtered := make([]asserts.Assertion, 0, len(assertions))
	for _, a := range assertions {
		a1 := filter(a)
		if a1 != nil {
			filtered = append(filtered, a1)
		}
	}
	seedtest.WriteAssertions(fn, filtered...)
}

func (s *seed20Suite) TestLoadAssertionsUnbalancedDeclsAndRevs(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("core20") {
			return nil
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `system unexpectedly holds a different number of snap-declaration than snap-revision assertions`)
}

func (s *seed20Suite) TestLoadAssertionsMultiSnapRev(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	spuriousRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     "1000",
		"snap-id":       s.AssertedSnapID("core20"),
		"developer-id":  "canonical",
		"snap-revision": "99",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("snapd") {
			return spuriousRev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `cannot have multiple snap-revisions for the same snap-id: core20ididididididididididididid`)
}

func (s *seed20Suite) TestLoadAssertionsMultiSnapDecl(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	spuriousDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "idididididididididididididididid",
		"publisher-id": "canonical",
		"snap-name":    "core20",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	spuriousRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     "1000",
		"snap-id":       s.AssertedSnapID("core20"),
		"developer-id":  "canonical",
		"snap-revision": "99",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "snapd" {
			return spuriousDecl
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("snapd") {
			return spuriousRev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `cannot have multiple snap-declarations for the same snap-name: core20`)
}

func (s *seed20Suite) TestLoadMetaMissingSnapDeclByName(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	wrongDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "idididididididididididididididid",
		"publisher-id": "canonical",
		"snap-name":    "core20X",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	wrongRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     "1000",
		"snap-id":       "idididididididididididididididid",
		"developer-id":  "canonical",
		"snap-revision": "99",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "core20" {
			return wrongDecl
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("core20") {
			return wrongRev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot find snap-declaration for snap name: core20`)
}

func (s *seed20Suite) TestLoadMetaMissingSnapDeclByID(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	wrongDecl, err := s.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "idididididididididididididididid",
		"publisher-id": "canonical",
		"snap-name":    "pc",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	wrongRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     "1000",
		"snap-id":       "idididididididididididididididid",
		"developer-id":  "canonical",
		"snap-revision": "99",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "pc" {
			return wrongDecl
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return wrongRev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot find snap-declaration for snap-id: pcididididididididididididididid`)
}

func (s *seed20Suite) TestLoadMetaMissingSnap(c *C) {
	sysLabel := "20191031"
	s.makeCore20MinimalSeed(c, sysLabel)

	err := os.Remove(filepath.Join(s.SeedDir, "snaps", "pc_1.snap"))
	c.Assert(err, IsNil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot stat snap:.*pc_1\.snap.*`)
}

func (s *seed20Suite) TestLoadMetaWrongSizeSnap(c *C) {
	sysLabel := "20191031"
	s.makeCore20MinimalSeed(c, sysLabel)

	err := os.Truncate(filepath.Join(s.SeedDir, "snaps", "pc_1.snap"), 5)
	c.Assert(err, IsNil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot validate ".*pc_1\.snap" for snap "pc" \(snap-id "pc.*"\), wrong size`)
}

func (s *seed20Suite) TestLoadMetaWrongHashSnap(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	pcRev := s.AssertedSnapRevision("pc")
	wrongRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     pcRev.HeaderString("snap-size"),
		"snap-id":       s.AssertedSnapID("pc"),
		"developer-id":  "canonical",
		"snap-revision": pcRev.HeaderString("snap-revision"),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return wrongRev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot validate ".*pc_1\.snap" for snap "pc" \(snap-id "pc.*"\), hash mismatch with snap-revision`)
}

func (s *seed20Suite) TestLoadMetaWrongGadgetBase(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	// pc with base: core18
	pc18Decl, pc18Rev := s.MakeAssertedSnap(c, snapYaml["pc=18"], nil, snap.R(2), "canonical")
	err := os.Rename(s.AssertedSnap("pc"), filepath.Join(s.SeedDir, "snaps", "pc_2.snap"))
	c.Assert(err, IsNil)
	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "pc" {
			return pc18Decl
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return pc18Rev
		}
		return a
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(s.perfTimings)
	c.Check(err, ErrorMatches, `cannot use gadget snap because its base "core18" is different from model base "core20"`)
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
	}, nil)

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

func (s *seed20Suite) makeLocalSnap(c *C, yamlKey string) (fname string) {
	return snaptest.MakeTestSnapWithFiles(c, snapYaml[yamlKey], nil)
}

func (s *seed20Suite) TestLoadMetaCore20LocalSnaps(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	sysLabel := "20101030"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
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
			}},
	}, []*seedwriter.OptionsSnap{
		{Path: requiredFn},
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
			Path:     filepath.Join(s.SeedDir, "systems", sysLabel, "snaps", "required20_1.0.snap"),
			SideInfo: &snap.SideInfo{RealName: "required20"},
			Required: true,
		},
	})
}

func (s *seed20Suite) TestLoadMetaCore20ChannelOverride(c *C) {
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
			}},
	}, []*seedwriter.OptionsSnap{
		{Name: "pc", Channel: "20experimental/edge"},
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
			Channel:   "20experimental/edge",
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
