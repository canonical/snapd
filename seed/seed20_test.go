// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2023 Canonical Ltd
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
	"crypto"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	"github.com/snapcore/snapd/timings"
)

type testSnapHandler struct {
	seedDir    string
	mu         sync.Mutex
	pathPrefix string
	asserted   map[string]string
	unasserted map[string]string
}

func newTestSnapHandler(seedDir string) *testSnapHandler {
	return &testSnapHandler{
		seedDir:    seedDir,
		asserted:   make(map[string]string),
		unasserted: make(map[string]string),
	}
}

func (h *testSnapHandler) rel(path string) string {
	p, err := filepath.Rel(h.seedDir, path)
	if err != nil {
		panic(err)
	}
	return p
}

func (h *testSnapHandler) HandleUnassertedContainer(cpi snap.ContainerPlaceInfo, path string, _ timings.Measurer) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unasserted[cpi.ContainerName()] = h.rel(path)
	return h.pathPrefix + path, nil
}

func (h *testSnapHandler) HandleAndDigestAssertedContainer(cpi snap.ContainerPlaceInfo, path string, _ timings.Measurer) (string, string, uint64, error) {
	snapSHA3_384, sz, err := asserts.SnapFileSHA3_384(path)
	if err != nil {
		return "", "", 0, err
	}
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.asserted[cpi.ContainerName()] = fmt.Sprintf("%s", h.rel(path))
	}()
	// XXX seed logic actually reads the gadget, leave it alone
	if cpi.ContainerName() != "pc" {
		path = h.pathPrefix + path
	}
	return path, snapSHA3_384, sz, err
}

type seed20Suite struct {
	testutil.BaseTest

	*seedtest.TestingSeed20
	devAcct *asserts.Account

	db *asserts.Database

	perfTimings timings.Measurer
}

var _ = Suite(&seed20Suite{})

var (
	otherbrandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *seed20Suite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	s.Brands.Register("other-brand", otherbrandPrivKey, nil)
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
	return filepath.Join(s.SeedDir, "snaps", s.AssertedSnapInfo(snapName).Filename())
}

func (s *seed20Suite) TestLoadMetaCore20Minimal(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20191018"
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	// check that PlaceInfo method works
	pi := essSnaps[0].PlaceInfo()
	c.Check(pi.Filename(), Equals, "snapd_1.snap")
	pi = essSnaps[1].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc-kernel_1.snap")
	pi = essSnaps[2].PlaceInfo()
	c.Check(pi.Filename(), Equals, "core20_1.snap")
	pi = essSnaps[3].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc_1.snap")

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)

	c.Check(seed20.NumSnaps(), Equals, 4)
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

	model := seed20.Model()
	c.Check(model.Model(), Equals, "my-model")
	c.Check(model.Base(), Equals, "core20")

	brand, err := seed20.Brand()
	c.Assert(err, IsNil)
	c.Check(brand.AccountID(), Equals, "my-brand")
	c.Check(brand.DisplayName(), Equals, "My-brand")
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

func (s *seed20Suite) massageAssertions(c *C, fn string, filter func(asserts.Assertion) []asserts.Assertion) {
	assertions := seedtest.ReadAssertions(c, fn)
	filtered := make([]asserts.Assertion, 0, len(assertions))
	for _, a := range assertions {
		a1 := filter(a)
		if a1 != nil {
			filtered = append(filtered, a1...)
		}
	}
	seedtest.WriteAssertions(fn, filtered...)
}

func (s *seed20Suite) TestLoadAssertionsUnbalancedDeclsAndRevs(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("core20") {
			return nil
		}
		return []asserts.Assertion{a}
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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("snapd") {
			return []asserts.Assertion{spuriousRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot have multiple snap-revisions for the same snap-id: %s`, s.AssertedSnapID("core20")))
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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "snapd" {
			return []asserts.Assertion{spuriousDecl}
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("snapd") {
			return []asserts.Assertion{spuriousRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `cannot have multiple snap-declarations for the same snap-name: core20`)
}

func (s *seed20Suite) TestLoadMetaMissingSnapDeclByName(c *C) {
	sysLabel := "20191031"

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

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
				"name": "core20",
				// no id
				"type": "base",
			}},
	}, nil)

	sysDir := filepath.Join(s.SeedDir, "systems", sysLabel)

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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "core20" {
			return []asserts.Assertion{wrongDecl}
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("core20") {
			return []asserts.Assertion{wrongRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "pc" {
			return []asserts.Assertion{wrongDecl}
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return []asserts.Assertion{wrongRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return []asserts.Assertion{wrongRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot validate ".*pc_1\.snap" for snap "pc" \(snap-id "pc.*"\), hash mismatch with snap-revision`)
}

func (s *seed20Suite) TestLoadMetaWrongGadgetBase(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	// pc with base: core18
	pc18Decl, pc18Rev := s.MakeAssertedSnap(c, snapYaml["pc=18"], nil, snap.R(2), "canonical")
	err := os.Rename(s.AssertedSnap("pc"), filepath.Join(s.SeedDir, "snaps", "pc_2.snap"))
	c.Assert(err, IsNil)
	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapDeclarationType && a.HeaderString("snap-name") == "pc" {
			return []asserts.Assertion{pc18Decl}
		}
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc") {
			return []asserts.Assertion{pc18Rev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot use gadget snap because its base "core18" is different from model base "core20"`)
}

func (s *seed20Suite) setSnapContact(snapName, contact string) {
	info := s.AssertedSnapInfo(snapName)
	info.EditedLinks = map[string][]string{
		"contact": {contact},
	}
	info.LegacyEditedContact = contact
}

func (s *seed20Suite) TestLoadMetaCore20(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.setSnapContact("required20", "mailto:author@example.com")

	sysLabel := "20191018"
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	c.Check(seed20.NumSnaps(), Equals, 5)

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

	// required20 has default modes: ["run"]
	installSnaps, err := seed20.ModeSnaps("install")
	c.Assert(err, IsNil)
	c.Check(installSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20DelegatedSnap(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)
	ra := map[string]interface{}{
		"account-id": "my-brand",
		"provenance": []interface{}{"delegated-prov"},
		"on-store":   []interface{}{"my-brand-store"},
	}
	s.MakeAssertedDelegatedSnap(c, snapYaml["required20"]+"\nprovenance: delegated-prov\n", nil, snap.R(1), "developerid", "my-brand", "delegated-prov", ra, s.StoreSigning.Database)

	s.setSnapContact("required20", "mailto:author@example.com")

	sysLabel := "20220705"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-brand-store",
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
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

	// required20 has default modes: ["run"]
	installSnaps, err := seed20.ModeSnaps("install")
	c.Assert(err, IsNil)
	c.Check(installSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20DelegatedSnapProvenanceMismatch(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)
	ra := map[string]interface{}{
		"account-id": "my-brand",
		"provenance": []interface{}{"delegated-prov"},
		"on-store":   []interface{}{"my-brand-store"},
	}
	s.MakeAssertedDelegatedSnap(c, snapYaml["required20"]+"\nprovenance: delegated-prov-other\n", nil, snap.R(1), "developerid", "my-brand", "delegated-prov", ra, s.StoreSigning.Database)

	sysLabel := "20220705"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-brand-store",
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `snap ".*required20_1\.snap" has been signed under provenance "delegated-prov" different from the metadata one: "delegated-prov-other"`)
}

func (s *seed20Suite) TestLoadMetaCore20DelegatedSnapDeviceMismatch(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)
	ra := map[string]interface{}{
		"account-id": "my-brand",
		"provenance": []interface{}{"delegated-prov"},
		"on-model":   []interface{}{"my-brand/my-other-model"},
	}
	s.MakeAssertedDelegatedSnap(c, snapYaml["required20"]+"\nprovenance: delegated-prov\n", nil, snap.R(1), "developerid", "my-brand", "delegated-prov", ra, s.StoreSigning.Database)

	sysLabel := "20220705"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"store":        "my-brand-store",
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `snap "required20" revision assertion with provenance "delegated-prov" is not signed by an authority authorized on this device: my-brand`)
}

func hideSnaps(c *C, all []*seed.Snap, keepTypes []snap.Type) (unhide func()) {
	var hidden [][]string
Hiding:
	for _, sn := range all {
		for _, t := range keepTypes {
			if sn.EssentialType == t {
				continue Hiding
			}
		}
		origFn := sn.Path
		hiddenFn := sn.Path + ".hidden"
		err := os.Rename(origFn, hiddenFn)
		c.Assert(err, IsNil)
		hidden = append(hidden, []string{origFn, hiddenFn})
	}
	return func() {
		for _, h := range hidden {
			err := os.Rename(h[1], h[0])
			c.Assert(err, IsNil)
		}
	}
}

func (s *seed20Suite) TestLoadEssentialMetaCore20(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "required18", "developerid")

	sysLabel := "20191018"
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
				"name": "core18",
				"id":   s.AssertedSnapID("core18"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "required18",
				"id":   s.AssertedSnapID("required18"),
			}},
	}, nil)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}
	core20Snap := &seed.Snap{Path: s.expectedPath("core20"),
		SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}
	core18Snap := &seed.Snap{
		// no EssentialType, so it's always hidden, shouldn't matter
		// because we should not look at it
		Path: s.expectedPath("core18"),
	}
	required18Snap := &seed.Snap{
		Path: s.expectedPath("required18"),
	}

	all := []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap, core18Snap, required18Snap}

	tests := []struct {
		onlyTypes []snap.Type
		expected  []*seed.Snap
	}{
		{[]snap.Type{snap.TypeSnapd}, []*seed.Snap{snapdSnap}},
		{[]snap.Type{snap.TypeKernel}, []*seed.Snap{pcKernelSnap}},
		{[]snap.Type{snap.TypeBase}, []*seed.Snap{core20Snap}},
		{[]snap.Type{snap.TypeGadget}, []*seed.Snap{pcSnap}},
		{[]snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase}, []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap}},
		// the order in essentialTypes is not relevant
		{[]snap.Type{snap.TypeGadget, snap.TypeKernel}, []*seed.Snap{pcKernelSnap, pcSnap}},
		// degenerate case
		{[]snap.Type{}, []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap}},
		{nil, []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap}},
	}

	for _, t := range tests {
		// hide the non-requested snaps to make sure they are not
		// accessed
		var unhide func()
		if len(t.onlyTypes) != 0 {
			unhide = hideSnaps(c, all, t.onlyTypes)
		}

		seed20, err := seed.Open(s.SeedDir, sysLabel)
		c.Assert(err, IsNil)

		err = seed20.LoadAssertions(nil, nil)
		c.Assert(err, IsNil)

		err = seed20.LoadEssentialMeta(t.onlyTypes, s.perfTimings)
		c.Assert(err, IsNil)

		c.Check(seed20.UsesSnapdSnap(), Equals, true)

		essSnaps := seed20.EssentialSnaps()
		c.Check(essSnaps, HasLen, len(t.expected))

		c.Check(essSnaps, DeepEquals, t.expected)

		runSnaps, err := seed20.ModeSnaps("run")
		c.Assert(err, IsNil)
		c.Check(runSnaps, HasLen, 0)

		if unhide != nil {
			unhide()
		}

		// test short-cut helper as well
		mod, essSnaps, err := seed.ReadSystemEssential(s.SeedDir, sysLabel, t.onlyTypes, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(mod.BrandID(), Equals, "my-brand")
		c.Check(mod.Model(), Equals, "my-model")
		c.Check(essSnaps, HasLen, len(t.expected))
		c.Check(essSnaps, DeepEquals, t.expected)
	}
}

func (s *seed20Suite) TestLoadEssentialMetaWithSnapHandlerCore20(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "required18", "developerid")

	sysLabel := "20191018"
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
				"name": "core18",
				"id":   s.AssertedSnapID("core18"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "required18",
				"id":   s.AssertedSnapID("required18"),
			}},
	}, nil)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}
	core20Snap := &seed.Snap{Path: s.expectedPath("core20"),
		SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}

	expected := []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap}

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	h := newTestSnapHandler(s.SeedDir)

	err = seed20.LoadEssentialMetaWithSnapHandler(nil, h, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, len(expected))
	c.Check(essSnaps, DeepEquals, expected)

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.snap",
		"pc-kernel": "snaps/pc-kernel_1.snap",
		"core20":    "snaps/core20_1.snap",
		"pc":        "snaps/pc_1.snap",
	})
}

func (s *seed20Suite) TestReadSystemEssentialAndBetterEarliestTime(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core18", "")
	t0 := time.Now().UTC().Truncate(time.Second)
	s.SetSnapAssertionNow(t0.Add(2 * time.Second))
	s.makeSnap(c, "required18", "developerid")
	s.SetSnapAssertionNow(time.Time{})

	baseLabel := "20210315"

	testReadSystemEssentialAndBetterEarliestTime := func(sysLabel string, earliestTime, modelTime, improvedTime time.Time) {
		s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
			"display-name": "my model",
			"timestamp":    modelTime.Format(time.RFC3339),
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
					"name": "core18",
					"id":   s.AssertedSnapID("core18"),
					"type": "base",
				},
				map[string]interface{}{
					"name": "required18",
					"id":   s.AssertedSnapID("required18"),
				}},
		}, nil)

		// test short-cut helper as well
		theSeed, betterTime, err := seed.ReadSeedAndBetterEarliestTime(s.SeedDir, sysLabel, earliestTime, 0, s.perfTimings)
		c.Assert(err, IsNil)
		c.Check(theSeed.Model().BrandID(), Equals, "my-brand")
		c.Check(theSeed.Model().Model(), Equals, "my-model")
		c.Check(theSeed.Model().Timestamp().Equal(modelTime), Equals, true)
		c.Check(betterTime.Equal(improvedTime), Equals, true, Commentf("%v expected: %v", betterTime, improvedTime))
	}

	revsTime := s.AssertedSnapRevision("required18").Timestamp()
	t2 := revsTime.Add(1 * time.Second)

	timeCombos := []struct {
		earliestTime, modelTime, improvedTime time.Time
	}{
		{time.Time{}, t0, revsTime},
		{t2.AddDate(-1, 0, 0), t0, revsTime},
		{t2.AddDate(-1, 0, 0), t2, t2},
		{t2.AddDate(0, 1, 0), t2, t2.AddDate(0, 1, 0)},
	}

	for i, c := range timeCombos {
		label := fmt.Sprintf("%s%d", baseLabel, i)
		testReadSystemEssentialAndBetterEarliestTime(label, c.earliestTime, c.modelTime, c.improvedTime)
	}
}

func (s *seed20Suite) TestReadSystemEssentialAndBetterEarliestTimeParallelism(c *C) {
	var testSeed *seed.TestSeed20
	restore := seed.MockOpen(func(seedDir, label string) (seed.Seed, error) {
		sd, err := seed.Open(seedDir, label)
		testSeed = seed.NewTestSeed20(sd)
		return testSeed, err
	})
	defer restore()

	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20191018"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"timestamp":    time.Now().Format(time.RFC3339),
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

	_, _, err := seed.ReadSeedAndBetterEarliestTime(s.SeedDir, sysLabel, time.Time{}, 3, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(testSeed, NotNil)
	c.Check(testSeed.Jobs, Equals, 3)
}

func (s *seed20Suite) TestLoadEssentialAndMetaCore20(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "required18", "developerid")

	sysLabel := "20191018"
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
				"name": "core18",
				"id":   s.AssertedSnapID("core18"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "required18",
				"id":   s.AssertedSnapID("required18"),
			}},
	}, nil)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}
	core20Snap := &seed.Snap{Path: s.expectedPath("core20"),
		SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "latest/stable",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "20",
	}

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	err = seed20.LoadEssentialMeta([]snap.Type{snap.TypeGadget}, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{pcSnap})

	err = seed20.LoadEssentialMeta([]snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase, snap.TypeGadget}, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps = seed20.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)

	// caching in place
	hideSnaps(c, []*seed.Snap{snapdSnap, core20Snap, pcKernelSnap}, nil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps = seed20.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{snapdSnap, pcKernelSnap, core20Snap, pcSnap})

	runSnaps, err = seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 2)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("core18"),
			SideInfo: &s.AssertedSnapInfo("core18").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		}, {
			Path:     s.expectedPath("required18"),
			SideInfo: &s.AssertedSnapInfo("required18").SideInfo,
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

	sysLabel := "20191030"
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
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

func (s *seed20Suite) TestLoadMetaCore20SnapHandler(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	sysLabel := "20191030"
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

	h := newTestSnapHandler(s.SeedDir)

	err = seed20.LoadMeta(seed.AllModes, h, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
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

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.snap",
		"pc-kernel": "snaps/pc-kernel_1.snap",
		"core20":    "snaps/core20_1.snap",
		"pc":        "snaps/pc_1.snap",
	})
	c.Check(h.unasserted, DeepEquals, map[string]string{
		"required20": filepath.Join("systems", sysLabel, "snaps", "required20_1.0.snap"),
	})
}

func (s *seed20Suite) TestLoadMetaCore20SnapHandlerChangePath(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	sysLabel := "20191030"
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

	h := newTestSnapHandler(s.SeedDir)
	h.pathPrefix = "/tmp/.."

	err = seed20.LoadMeta(seed.AllModes, h, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          "/tmp/.." + s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          "/tmp/.." + s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          "/tmp/.." + s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)

	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     "/tmp/.." + filepath.Join(s.SeedDir, "systems", sysLabel, "snaps", "required20_1.0.snap"),
			SideInfo: &snap.SideInfo{RealName: "required20"},
			Required: true,
		},
	})

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.snap",
		"pc-kernel": "snaps/pc-kernel_1.snap",
		"core20":    "snaps/core20_1.snap",
		"pc":        "snaps/pc_1.snap",
	})
	c.Check(h.unasserted, DeepEquals, map[string]string{
		"required20": filepath.Join("systems", sysLabel, "snaps", "required20_1.0.snap"),
	})
}

func (s *seed20Suite) TestLoadMetaCore20ChannelOverride(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.setSnapContact("required20", "mailto:author@example.com")

	sysLabel := "20191018"
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20experimental/edge",
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

func (s *seed20Suite) TestLoadMetaCore20ChannelOverrideSnapd(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.setSnapContact("required20", "mailto:author@example.com")

	sysLabel := "20191121"
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
		{Name: "snapd", Channel: "20experimental/edge"},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "20experimental/edge",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
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

func (s *seed20Suite) TestLoadMetaCore20LocalSnapd(c *C) {
	snapdFn := s.makeLocalSnap(c, "snapd")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20191121"
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
			}},
	}, []*seedwriter.OptionsSnap{
		{Path: snapdFn},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          filepath.Join(s.SeedDir, "systems", sysLabel, "snaps", "snapd_1.0.snap"),
			SideInfo:      &snap.SideInfo{RealName: "snapd"},
			Essential:     true,
			EssentialType: snap.TypeSnapd,
			Required:      true,
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20ModelOverrideSnapd(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20191121"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "snapd",
				"type":            "snapd",
				"default-channel": "latest/edge",
			},
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/edge",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20OptionalSnaps(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "optional20-a", "developerid")
	s.makeSnap(c, "optional20-b", "developerid")

	sysLabel := "20191122"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "signed",
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
				"name":     "optional20-a",
				"id":       s.AssertedSnapID("optional20-a"),
				"presence": "optional",
			},
			map[string]interface{}{
				"name":     "optional20-b",
				"id":       s.AssertedSnapID("optional20-b"),
				"presence": "optional",
			}},
	}, []*seedwriter.OptionsSnap{
		{Name: "optional20-b"},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("optional20-b"),
			SideInfo: &s.AssertedSnapInfo("optional20-b").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
	})
}

func (s *seed20Suite) TestLoadMetaCore20OptionalSnapsLocal(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "optional20-a", "developerid")
	optional20bFn := s.makeLocalSnap(c, "optional20-b")

	sysLabel := "20191122"
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
				"name":     "optional20-a",
				"id":       s.AssertedSnapID("optional20-a"),
				"presence": "optional",
			},
			map[string]interface{}{
				"name":     "optional20-b",
				"id":       s.AssertedSnapID("optional20-b"),
				"presence": "optional",
			}},
	}, []*seedwriter.OptionsSnap{
		{Path: optional20bFn},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join(s.SeedDir, "systems", sysLabel, "snaps", "optional20-b_1.0.snap"),
			SideInfo: &snap.SideInfo{RealName: "optional20-b"},

			Required: false,
		},
	})
}

func (s *seed20Suite) TestLoadMetaCore20ExtraSnaps(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "core18", "")
	s.makeSnap(c, "cont-producer", "developerid")
	contConsumerFn := s.makeLocalSnap(c, "cont-consumer")

	sysLabel := "20191122"
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
			}},
	}, []*seedwriter.OptionsSnap{
		{Name: "cont-producer", Channel: "edge"},
		{Name: "core18"},
		{Path: contConsumerFn},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	sysSnapsDir := filepath.Join(s.SeedDir, "systems", sysLabel, "snaps")

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 3)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join(sysSnapsDir, "cont-producer_1.snap"),
			SideInfo: &s.AssertedSnapInfo("cont-producer").SideInfo,
			Channel:  "latest/edge",
		},
		{
			Path:     filepath.Join(sysSnapsDir, "core18_1.snap"),
			SideInfo: &s.AssertedSnapInfo("core18").SideInfo,
			Channel:  "latest/stable",
		},
		{
			Path:     filepath.Join(sysSnapsDir, "cont-consumer_1.0.snap"),
			SideInfo: &snap.SideInfo{RealName: "cont-consumer"},
		},
	})

	recoverSnaps, err := seed20.ModeSnaps("recover")
	c.Assert(err, IsNil)
	c.Check(recoverSnaps, HasLen, 0)
}

func (s *seed20Suite) TestLoadMetaCore20NotRunSnaps(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")
	s.makeSnap(c, "optional20-a", "developerid")
	s.makeSnap(c, "optional20-b", "developerid")

	sysLabel := "20191122"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "signed",
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
				"name":  "required20",
				"id":    s.AssertedSnapID("required20"),
				"modes": []interface{}{"run", "ephemeral"},
			},
			map[string]interface{}{
				"name":     "optional20-a",
				"id":       s.AssertedSnapID("optional20-a"),
				"presence": "optional",
				"modes":    []interface{}{"ephemeral"},
			},
			map[string]interface{}{
				"name":     "optional20-b",
				"id":       s.AssertedSnapID("optional20-b"),
				"presence": "optional",
				"modes":    []interface{}{"install"},
			}},
	}, []*seedwriter.OptionsSnap{
		{Name: "optional20-a"},
		{Name: "optional20-b"},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	c.Check(seed20.NumSnaps(), Equals, 7)

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

	installSnaps, err := seed20.ModeSnaps("install")
	c.Assert(err, IsNil)
	c.Check(installSnaps, HasLen, 3)
	c.Check(installSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required20"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-a"),
			SideInfo: &s.AssertedSnapInfo("optional20-a").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-b"),
			SideInfo: &s.AssertedSnapInfo("optional20-b").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
	})

	recoverSnaps, err := seed20.ModeSnaps("recover")
	c.Assert(err, IsNil)
	c.Check(recoverSnaps, HasLen, 2)
	c.Check(recoverSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required20"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-a"),
			SideInfo: &s.AssertedSnapInfo("optional20-a").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
	})
}

func (s *seed20Suite) TestLoadMetaCore20PreciseNotRunSnaps(c *C) {
	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 1, nil)
}

func (s *seed20Suite) TestLoadMetaCore20PreciseNotRunSnapsSnapHandler(c *C) {
	runH := newTestSnapHandler(s.SeedDir)
	installH := newTestSnapHandler(s.SeedDir)
	recoverH := newTestSnapHandler(s.SeedDir)
	handlers := map[string]seed.ContainerHandler{
		"install": installH,
		"run":     runH,
		"recover": recoverH,
	}

	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 1, handlers)

	c.Check(installH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap",
		"pc-kernel":    "snaps/pc-kernel_1.snap",
		"core20":       "snaps/core20_1.snap",
		"pc":           "snaps/pc_1.snap",
		"required20":   "snaps/required20_1.snap",
		"optional20-a": "snaps/optional20-a_1.snap",
		"optional20-b": "snaps/optional20-b_1.snap",
	})
	c.Check(runH.asserted, DeepEquals, map[string]string{
		"snapd":      "snaps/snapd_1.snap",
		"pc-kernel":  "snaps/pc-kernel_1.snap",
		"core20":     "snaps/core20_1.snap",
		"pc":         "snaps/pc_1.snap",
		"required20": "snaps/required20_1.snap",
	})
	c.Check(recoverH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap",
		"pc-kernel":    "snaps/pc-kernel_1.snap",
		"core20":       "snaps/core20_1.snap",
		"pc":           "snaps/pc_1.snap",
		"required20":   "snaps/required20_1.snap",
		"optional20-a": "snaps/optional20-a_1.snap",
	})
}

func (s *seed20Suite) testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c *C, parallelism int, handlers map[string]seed.ContainerHandler) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")
	s.makeSnap(c, "optional20-a", "developerid")
	s.makeSnap(c, "optional20-b", "developerid")

	sysLabel := "20191122"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "signed",
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
				"name":  "required20",
				"id":    s.AssertedSnapID("required20"),
				"modes": []interface{}{"run", "ephemeral"},
			},
			map[string]interface{}{
				"name":     "optional20-a",
				"id":       s.AssertedSnapID("optional20-a"),
				"presence": "optional",
				"modes":    []interface{}{"ephemeral"},
			},
			map[string]interface{}{
				"name":     "optional20-b",
				"id":       s.AssertedSnapID("optional20-b"),
				"presence": "optional",
				"modes":    []interface{}{"install"},
			}},
	}, []*seedwriter.OptionsSnap{
		{Name: "optional20-a"},
		{Name: "optional20-b"},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	seed20.SetParallelism(parallelism)

	err = seed20.LoadMeta("install", handlers["install"], s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	c.Check(seed20.NumSnaps(), Equals, 7)

	installSnaps, err := seed20.ModeSnaps("install")
	c.Assert(err, IsNil)
	c.Check(installSnaps, HasLen, 3)
	c.Check(installSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required20"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-a"),
			SideInfo: &s.AssertedSnapInfo("optional20-a").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-b"),
			SideInfo: &s.AssertedSnapInfo("optional20-b").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
	})

	_, err = seed20.ModeSnaps("recover")
	c.Check(err, ErrorMatches, `metadata was loaded only for snaps for mode install not recover`)
	_, err = seed20.ModeSnaps("run")
	c.Check(err, ErrorMatches, `metadata was loaded only for snaps for mode install not run`)

	err = seed20.LoadMeta("recover", handlers["recover"], s.perfTimings)
	c.Assert(err, IsNil)
	// only recover mode snaps
	c.Check(seed20.NumSnaps(), Equals, 6)

	recoverSnaps, err := seed20.ModeSnaps("recover")
	c.Assert(err, IsNil)
	c.Check(recoverSnaps, HasLen, 2)
	c.Check(recoverSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required20"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Required: true,
			Channel:  "latest/stable",
		},
		{
			Path:     s.expectedPath("optional20-a"),
			SideInfo: &s.AssertedSnapInfo("optional20-a").SideInfo,
			Required: false,
			Channel:  "latest/stable",
		},
	})

	err = seed20.LoadMeta("run", handlers["run"], s.perfTimings)
	c.Assert(err, IsNil)
	// only run mode snaps
	c.Check(seed20.NumSnaps(), Equals, 5)

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

func (s *seed20Suite) TestLoadMetaCore20PreciseNotRunSnapsParallelism2(c *C) {
	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 2, nil)
}

func (s *seed20Suite) TestLoadMetaCore20PreciseNotRunSnapsParallelism2SnapHandler(c *C) {
	runH := newTestSnapHandler(s.SeedDir)
	installH := newTestSnapHandler(s.SeedDir)
	recoverH := newTestSnapHandler(s.SeedDir)
	handlers := map[string]seed.ContainerHandler{
		"install": installH,
		"run":     runH,
		"recover": recoverH,
	}
	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 2, handlers)

	c.Check(installH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap",
		"pc-kernel":    "snaps/pc-kernel_1.snap",
		"core20":       "snaps/core20_1.snap",
		"pc":           "snaps/pc_1.snap",
		"required20":   "snaps/required20_1.snap",
		"optional20-a": "snaps/optional20-a_1.snap",
		"optional20-b": "snaps/optional20-b_1.snap",
	})
	c.Check(runH.asserted, DeepEquals, map[string]string{
		"snapd":      "snaps/snapd_1.snap",
		"pc-kernel":  "snaps/pc-kernel_1.snap",
		"core20":     "snaps/core20_1.snap",
		"pc":         "snaps/pc_1.snap",
		"required20": "snaps/required20_1.snap",
	})
	c.Check(recoverH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap",
		"pc-kernel":    "snaps/pc-kernel_1.snap",
		"core20":       "snaps/core20_1.snap",
		"pc":           "snaps/pc_1.snap",
		"required20":   "snaps/required20_1.snap",
		"optional20-a": "snaps/optional20-a_1.snap",
	})
}

func (s *seed20Suite) TestLoadMetaCore20LocalAssertedSnaps(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	sysLabel := "20191209"
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
			}},
	}, []*seedwriter.OptionsSnap{
		{Path: s.AssertedSnap("pc"), Channel: "edge"},
		{Path: s.AssertedSnap("required20")},
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20/edge",
		},
	})

	sysSnapsDir := filepath.Join(s.SeedDir, "systems", sysLabel, "snaps")

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join(sysSnapsDir, "required20_1.snap"),
			SideInfo: &s.AssertedSnapInfo("required20").SideInfo,
			Channel:  "latest/stable",
		},
	})
}

func (s *seed20Suite) TestOpenInvalidLabel(c *C) {
	invalid := []string{
		// empty string not included, as it's not a UC20 seed
		"/bin",
		"../../bin/bar",
		":invalid:",
		"日本語",
	}
	for _, label := range invalid {
		seed20, err := seed.Open(s.SeedDir, label)
		c.Assert(err, ErrorMatches, fmt.Sprintf("invalid seed system label: %q", label))
		c.Assert(seed20, IsNil)
	}
}

func (s *seed20Suite) TestLoadMetaCore20Iter(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	sysLabel := "20191209"
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
			},
		},
	}, nil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.NumSnaps(), Equals, 5)

	// iterates over all snaps
	seen := map[string]bool{}
	err = seed20.Iter(func(sn *seed.Snap) error {
		seen[sn.SnapName()] = true
		return nil
	})
	c.Assert(err, IsNil)
	c.Check(seen, DeepEquals, map[string]bool{
		"snapd":      true,
		"pc-kernel":  true,
		"core20":     true,
		"pc":         true,
		"required20": true,
	})

	// and bubbles up the errors
	err = seed20.Iter(func(sn *seed.Snap) error {
		if sn.SnapName() == "core20" {
			return fmt.Errorf("mock error for snap %q", sn.SnapName())
		}
		return nil
	})
	c.Assert(err, ErrorMatches, `mock error for snap "core20"`)
}

func (s *seed20Suite) TestLoadMetaWrongHashSnapParallelism2(c *C) {
	sysLabel := "20191031"
	sysDir := s.makeCore20MinimalSeed(c, sysLabel)

	pcKernelRev := s.AssertedSnapRevision("pc-kernel")
	wrongRev, err := s.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": strings.Repeat("B", 64),
		"snap-size":     pcKernelRev.HeaderString("snap-size"),
		"snap-id":       s.AssertedSnapID("pc-kernel"),
		"developer-id":  "canonical",
		"snap-revision": pcKernelRev.HeaderString("snap-revision"),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) []asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc-kernel") {
			return []asserts.Assertion{wrongRev}
		}
		return []asserts.Assertion{a}
	})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	seed20.SetParallelism(2)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot validate ".*pc-kernel_1\.snap" for snap "pc-kernel" \(snap-id "pckernel.*"\), hash mismatch with snap-revision`)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeSecuredNoAutoImportAssertion(c *C) {
	// secured grade, no system user assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelSecured, none, 0644, s.commitTo, nil)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeSecuredAutoImportAssertion(c *C) {
	// secured grade, with system user assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelSecured, valid, 0644, s.commitTo, nil)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousNoAutoImportAssertion(c *C) {
	// dangerous grade, no system user assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, none, 0644, s.commitTo, fmt.Errorf("*. no such file or directory"))
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionErrCommiter(c *C) {
	// dangerous grade with broken commiter
	err := fmt.Errorf("nope")
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, valid, 0644, func(b *asserts.Batch) error {
		return err
	}, err)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionErrFilePerm(c *C) {
	// dangerous grade, system user assertion with wrong file permissions
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, valid, 0222, s.commitTo, fmt.Errorf(".* permission denied"))
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousInvalidAutoImportAssertion(c *C) {
	// dangerous grade, invalid system user assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, invalid, 0644, s.commitTo, fmt.Errorf("unexpected EOF"))
}

type systemUserAssertion int

const (
	none systemUserAssertion = iota
	valid
	invalid
)

func (s *seed20Suite) testLoadAutoImportAssertion(c *C, grade asserts.ModelGrade, sua systemUserAssertion, perm os.FileMode, commitTo func(b *asserts.Batch) error, loadError error) {
	sysLabel := "20191018"
	seed20 := s.createMinimalSeed(c, string(grade), sysLabel)
	c.Assert(seed20, NotNil)
	c.Check(seed20.Model().Grade(), Equals, grade)

	// write test auto import assertion
	switch sua {
	case valid:
		seedtest.WriteValidAutoImportAssertion(c, s.Brands, s.SeedDir, sysLabel, perm)
	case invalid:
		s.writeInvalidAutoImportAssertion(c, sysLabel, perm)
	}

	// try to load auto import assertions
	seed20AsLoader, ok := seed20.(seed.AutoImportAssertionsLoaderSeed)
	c.Assert(ok, Equals, true)
	err := seed20AsLoader.LoadAutoImportAssertions(commitTo)
	if loadError == nil {
		c.Assert(err, IsNil)
	} else {
		c.Check(err, ErrorMatches, loadError.Error())
	}
	assertions, err := s.findAutoImportAssertion(seed20)
	c.Check(err, ErrorMatches, "system-user assertion not found")
	c.Assert(assertions, IsNil)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionHappy(c *C) {
	sysLabel := "20191018"
	seed20 := s.createMinimalSeed(c, "dangerous", sysLabel)
	c.Assert(seed20, NotNil)
	c.Check(seed20.Model().Grade(), Equals, asserts.ModelDangerous)

	seedtest.WriteValidAutoImportAssertion(c, s.Brands, s.SeedDir, sysLabel, 0644)

	// try to load auto import assertions
	seed20AsLoader, ok := seed20.(seed.AutoImportAssertionsLoaderSeed)
	c.Assert(ok, Equals, true)
	err := seed20AsLoader.LoadAutoImportAssertions(s.commitTo)
	c.Assert(err, IsNil)
	assertions, err := s.findAutoImportAssertion(seed20)
	c.Assert(err, IsNil)
	// validate it's our assertion
	c.Check(len(assertions), Equals, 1)
	systemUser := assertions[0].(*asserts.SystemUser)
	c.Check(systemUser.Username(), Equals, "guy")
	c.Check(systemUser.Email(), Equals, "foo@bar.com")
	c.Check(systemUser.Name(), Equals, "Boring Guy")
	c.Check(systemUser.AuthorityID(), Equals, "my-brand")
}

func (s *seed20Suite) createMinimalSeed(c *C, grade string, sysLabel string) seed.Seed {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name":          "my model",
		"architecture":          "amd64",
		"base":                  "core20",
		"grade":                 grade,
		"system-user-authority": []interface{}{"my-brand", "other-brand"},
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

	return seed20
}

func (s *seed20Suite) writeInvalidAutoImportAssertion(c *C, sysLabel string, perm os.FileMode) {
	autoImportAssert := filepath.Join(s.SeedDir, "systems", sysLabel, "auto-import.assert")
	// write invalid data
	err := os.WriteFile(autoImportAssert, []byte(strings.Repeat("a", 512)), perm)
	c.Assert(err, IsNil)
}

// findAutoImportAssertion returns found systemUser assertion
func (s *seed20Suite) findAutoImportAssertion(seed20 seed.Seed) ([]asserts.Assertion, error) {
	assertions, err := s.db.FindMany(asserts.SystemUserType, map[string]string{
		"brand-id": seed20.Model().BrandID(),
	})

	return assertions, err
}

func (s *seed20Suite) TestPreseedCapableSeed(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20230406"
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

	preseedArtifact := filepath.Join(s.SeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	snaps := []interface{}{
		map[string]interface{}{"name": "snapd", "id": s.AssertedSnapID("snapd"), "revision": "1"},
		map[string]interface{}{"name": "core20", "id": s.AssertedSnapID("core20"), "revision": "1"},
		map[string]interface{}{"name": "pc-kernel", "id": s.AssertedSnapID("pc-kernel"), "revision": "1"},
		map[string]interface{}{"name": "pc", "id": s.AssertedSnapID("pc"), "revision": "1"},
	}
	headers := map[string]interface{}{
		"type":              "preseed",
		"series":            "16",
		"brand-id":          "my-brand",
		"model":             "my-model",
		"system-label":      sysLabel,
		"artifact-sha3-384": digest,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"snaps":             snaps,
	}

	signer := s.Brands.Signing("my-brand")
	preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
	c.Assert(err, IsNil)
	seedtest.WriteAssertions(filepath.Join(s.SeedDir, "systems", sysLabel, "preseed"), preseedAs)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	preseedSeed := seed20.(seed.PreseedCapable)

	c.Check(preseedSeed.HasArtifact("preseed.tgz"), Equals, true)
	c.Check(preseedSeed.HasArtifact("other.tgz"), Equals, false)

	err = preseedSeed.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	err = preseedSeed.LoadEssentialMeta(nil, s.perfTimings)
	c.Assert(err, IsNil)

	preesedAs2, err := preseedSeed.LoadPreseedAssertion()
	c.Assert(err, IsNil)
	c.Check(preesedAs2, DeepEquals, preseedAs)
}

func (s *seed20Suite) TestPreseedCapableSeedErrors(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20230406"
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

	preseedArtifact := filepath.Join(s.SeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	snaps := []interface{}{
		map[string]interface{}{"name": "snapd", "id": s.AssertedSnapID("snapd"), "revision": "1"},
		map[string]interface{}{"name": "core20", "id": s.AssertedSnapID("core20"), "revision": "1"},
		map[string]interface{}{"name": "pc-kernel", "id": s.AssertedSnapID("pc-kernel"), "revision": "1"},
		map[string]interface{}{"name": "pc", "id": s.AssertedSnapID("pc"), "revision": "1"},
	}

	tests := []struct {
		omitPreseedAssert bool
		dupPreseedAssert  bool

		overrides map[string]interface{}
		asserts   []asserts.Assertion
		err       string
	}{
		{omitPreseedAssert: true, asserts: s.Brands.AccountsAndKeys("my-brand"), err: `system preseed assertion file must contain a preseed assertion`},
		// this works for contrast
		{asserts: s.Brands.AccountsAndKeys("my-brand"), err: ""},
		{dupPreseedAssert: true, err: `system preseed assertion file cannot contain multiple preseed assertions`},
		{overrides: map[string]interface{}{"system-label": "other-label"}, err: `preseed assertion system label "other-label" doesn't match system label "20230406"`},
		{overrides: map[string]interface{}{"model": "other-model"}, err: `preseed assertion model "other-model" doesn't match the model "my-model"`},
		{overrides: map[string]interface{}{"series": "other-series"}, err: `preseed assertion series "other-series" doesn't match model series "16"`},
		{overrides: map[string]interface{}{"authority-id": "other-brand"}, asserts: s.Brands.AccountsAndKeys("other-brand"), err: `preseed authority-id "other-brand" is not allowed by the model`},
		{overrides: map[string]interface{}{"brand-id": "other-brand", "authority-id": "other-brand"}, err: `cannot resolve prerequisite assertion:.*`},
	}

	for _, tc := range tests {
		headers := map[string]interface{}{
			"type":              "preseed",
			"series":            "16",
			"brand-id":          "my-brand",
			"authority-id":      "my-brand",
			"model":             "my-model",
			"system-label":      sysLabel,
			"artifact-sha3-384": digest,
			"timestamp":         time.Now().UTC().Format(time.RFC3339),
			"snaps":             snaps,
		}
		as := tc.asserts
		if !tc.omitPreseedAssert {
			for h, v := range tc.overrides {
				headers[h] = v
			}
			signer := s.Brands.Signing(headers["authority-id"].(string))
			preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
			c.Assert(err, IsNil)
			as = append(as, preseedAs)
		}
		if tc.dupPreseedAssert {
			headers["system-label"] = "other-label"
			signer := s.Brands.Signing(headers["authority-id"].(string))
			preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
			c.Assert(err, IsNil)
			as = append(as, preseedAs)
		}
		seedtest.WriteAssertions(filepath.Join(s.SeedDir, "systems", sysLabel, "preseed"), as...)
		seed20, err := seed.Open(s.SeedDir, sysLabel)
		c.Assert(err, IsNil)
		preseedSeed := seed20.(seed.PreseedCapable)
		err = preseedSeed.LoadAssertions(nil, nil)
		c.Assert(err, IsNil)

		_, err = preseedSeed.LoadPreseedAssertion()
		if tc.err == "" {
			// contrast happy cases
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}

func (s *seed20Suite) TestPreseedCapableSeedNoPreseedAssertion(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20230406"
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

	preseedSeed := seed20.(seed.PreseedCapable)

	c.Check(preseedSeed.HasArtifact("preseed.tgz"), Equals, false)
	c.Check(preseedSeed.HasArtifact("other.tgz"), Equals, false)

	err = preseedSeed.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	err = preseedSeed.LoadEssentialMeta(nil, s.perfTimings)
	c.Assert(err, IsNil)

	_, err = preseedSeed.LoadPreseedAssertion()
	c.Assert(err, Equals, seed.ErrNoPreseedAssertion)
}

func (s *seed20Suite) TestPreseedCapableSeedAlternateAuthority(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")

	sysLabel := "20230406"
	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"preseed-authority": []interface{}{
			"my-brand",
			"my-signer",
		},
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

	preseedArtifact := filepath.Join(s.SeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.WriteFile(preseedArtifact, nil, 0644), IsNil)
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	snaps := []interface{}{
		map[string]interface{}{"name": "snapd", "id": s.AssertedSnapID("snapd"), "revision": "1"},
		map[string]interface{}{"name": "core20", "id": s.AssertedSnapID("core20"), "revision": "1"},
		map[string]interface{}{"name": "pc-kernel", "id": s.AssertedSnapID("pc-kernel"), "revision": "1"},
		map[string]interface{}{"name": "pc", "id": s.AssertedSnapID("pc"), "revision": "1"},
	}

	signerKey, _ := assertstest.GenerateKey(752)
	s.Brands.Register("my-signer", signerKey, nil)

	headers := map[string]interface{}{
		"type":              "preseed",
		"series":            "16",
		"brand-id":          "my-brand",
		"authority-id":      "my-signer",
		"model":             "my-model",
		"system-label":      sysLabel,
		"artifact-sha3-384": digest,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"snaps":             snaps,
	}
	signer := s.Brands.Signing("my-signer")
	preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
	c.Assert(err, IsNil)

	systemDir := filepath.Join(s.SeedDir, "systems", sysLabel)
	seedtest.WriteAssertions(
		filepath.Join(systemDir, "assertions", "my-signer"),
		s.Brands.AccountsAndKeys("my-signer")...,
	)
	seedtest.WriteAssertions(filepath.Join(systemDir, "preseed"), preseedAs)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	preseedSeed := seed20.(seed.PreseedCapable)

	c.Check(preseedSeed.HasArtifact("preseed.tgz"), Equals, true)

	err = preseedSeed.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	err = preseedSeed.LoadEssentialMeta(nil, s.perfTimings)
	c.Assert(err, IsNil)

	preseedAs2, err := preseedSeed.LoadPreseedAssertion()
	c.Assert(err, IsNil)
	c.Check(preseedAs2, DeepEquals, preseedAs)
}

func (s *seed20Suite) TestCopy(c *C) {
	const label = "20240126"
	s.testCopy(c, label)
}

func (s *seed20Suite) TestCopyEmptyLabel(c *C) {
	const label = ""
	s.testCopy(c, label)
}

func (s *seed20Suite) testCopy(c *C, destLabel string) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	const srcLabel = "20191030"
	s.MakeSeed(c, srcLabel, "my-brand", "my-model", map[string]interface{}{
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

	seed20, err := seed.Open(s.SeedDir, srcLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	copier, ok := seed20.(seed.Copier)
	c.Assert(ok, Equals, true)

	destSeedDir := c.MkDir()

	err = copier.Copy(destSeedDir, destLabel, s.perfTimings)
	c.Assert(err, IsNil)

	checkDirContents(c, filepath.Join(destSeedDir, "snaps"), []string{
		"core20_1.snap",
		"pc_1.snap",
		"pc-kernel_1.snap",
		"snapd_1.snap",
	})

	copiedLabel := destLabel
	if copiedLabel == "" {
		copiedLabel = srcLabel
	}

	destSystemDir := filepath.Join(destSeedDir, "systems", copiedLabel)

	checkDirContents(c, destSystemDir, []string{
		"assertions",
		"model",
		"options.yaml",
		"snaps",
	})

	checkDirContents(c, filepath.Join(destSystemDir, "assertions"), []string{
		"model-etc",
		"snaps",
	})

	checkDirContents(c, filepath.Join(destSystemDir, "snaps"), []string{
		"required20_1.0.snap",
	})

	compareDirs(c, filepath.Join(s.SeedDir, "snaps"), filepath.Join(destSeedDir, "snaps"))
	compareDirs(c, filepath.Join(s.SeedDir, "systems", srcLabel), destSystemDir)

	err = copier.Copy(destSeedDir, copiedLabel, s.perfTimings)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot create system: system %q already exists at %q`, copiedLabel, destSystemDir))
}

func (s *seed20Suite) TestCopyCleanup(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	requiredFn := s.makeLocalSnap(c, "required20")

	const label = "20191030"
	s.MakeSeed(c, label, "my-brand", "my-model", map[string]interface{}{
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

	seed20, err := seed.Open(s.SeedDir, label)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	copier, ok := seed20.(seed.Copier)
	c.Assert(ok, Equals, true)

	removedSnap := filepath.Join(s.SeedDir, "snaps", "snapd_1.snap")

	// remove a snap from the original seed to make the copy fail
	err = os.Remove(removedSnap)
	c.Assert(err, IsNil)

	destSeedDir := c.MkDir()
	err = copier.Copy(destSeedDir, label, s.perfTimings)
	c.Check(err, ErrorMatches, fmt.Sprintf("cannot stat snap: stat %s: no such file or directory", removedSnap))

	// seed destination should have been cleaned up
	c.Check(filepath.Join(destSeedDir, "systems", label), testutil.FileAbsent)
}

func checkDirContents(c *C, dir string, expected []string) {
	sort.Strings(expected)

	entries, err := os.ReadDir(dir)
	c.Assert(err, IsNil)

	found := make([]string, 0, len(entries))
	for _, e := range entries {
		found = append(found, e.Name())
	}

	c.Check(found, DeepEquals, expected)
}

func compareDirs(c *C, expected, got string) {
	expected, err := filepath.Abs(expected)
	c.Assert(err, IsNil)

	got, err = filepath.Abs(got)
	c.Assert(err, IsNil)

	expectedCount := 0
	err = filepath.WalkDir(expected, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		expectedCount++

		gotPath := filepath.Join(got, strings.TrimPrefix(path, expected))

		if d.IsDir() {
			c.Check(osutil.IsDirectory(gotPath), Equals, true)
			return nil
		}

		c.Check(gotPath, testutil.FileEquals, testutil.FileContentRef(path))

		return nil
	})
	c.Assert(err, IsNil)

	gotCount := 0
	err = filepath.WalkDir(got, func(_ string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		gotCount++
		return nil
	})
	c.Assert(err, IsNil)

	c.Check(gotCount, Equals, expectedCount)
}

func (s *seed20Suite) makeCore20SeedWithComps(c *C, sysLabel string) string {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	comRevs := map[string]snap.Revision{
		"comp1": snap.R(22),
		"comp2": snap.R(33),
	}
	s.MakeAssertedSnapWithComps(c, seedtest.SampleSnapYaml["required20"], nil,
		snap.R(11), comRevs, "canonical", s.StoreSigning.Database)

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
				"type": "app",
				"components": map[string]interface{}{
					"comp1": "required",
					"comp2": "required",
				},
			},
		},
	}, nil)

	return filepath.Join(s.SeedDir, "systems", sysLabel)
}

func (s *seed20Suite) TestLoadMetaWithComponents(c *C) {
	sysLabel := "20240805"
	s.makeCore20SeedWithComps(c, sysLabel)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	// check that PlaceInfo method works
	pi := essSnaps[0].PlaceInfo()
	c.Check(pi.Filename(), Equals, "snapd_1.snap")
	pi = essSnaps[1].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc-kernel_1.snap")
	pi = essSnaps[2].PlaceInfo()
	c.Check(pi.Filename(), Equals, "core20_1.snap")
	pi = essSnaps[3].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc_1.snap")

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	req20sn := runSnaps[0]
	c.Check(req20sn.SnapName(), Equals, "required20")
	c.Check(len(req20sn.Components), Equals, 2)
	checked := make([]bool, 2)
	for _, comp := range req20sn.Components {
		switch comp.CompSideInfo.Component.ComponentName {
		case "comp1":
			c.Check(comp, DeepEquals, seed.Component{
				Path: filepath.Join(s.SeedDir, "snaps", "required20+comp1_22.comp"),
				CompSideInfo: &snap.ComponentSideInfo{
					Component: naming.NewComponentRef("required20", "comp1"),
					Revision:  snap.R(22),
				},
			})
			checked[0] = true
		case "comp2":
			c.Check(comp, DeepEquals, seed.Component{
				Path: filepath.Join(s.SeedDir, "snaps", "required20+comp2_33.comp"),
				CompSideInfo: &snap.ComponentSideInfo{
					Component: naming.NewComponentRef("required20", "comp2"),
					Revision:  snap.R(33),
				},
			})
			checked[1] = true
		}
	}
	c.Check(checked, DeepEquals, []bool{true, true})

	c.Check(seed20.NumSnaps(), Equals, 5)
}

func (s *seed20Suite) TestLoadMetaWithComponentsNoAssertForReqComp(c *C) {
	sysLabel := "20240805"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	// Remove all assertions for comp2
	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp2" {
				return []asserts.Assertion{}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "component comp2 required in the model but is not in the seed: resource revision assertion not found for comp2")
}

func (s *seed20Suite) TestLoadMetaWithComponentsReqNotPresent(c *C) {
	sysLabel := "20240805"
	s.makeCore20SeedWithComps(c, sysLabel)

	// sneakly remove one of the components from the seed
	c.Assert(os.Remove(filepath.Join(s.SeedDir, "snaps", "required20+comp2_33.comp")), IsNil)

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "component comp2 required in the model but is not in the seed: .*no such file or directory")
}

func (s *seed20Suite) TestLoadMetaWithComponentsBadSize(c *C) {
	sysLabel := "20240805"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	finfo, err := os.Stat(filepath.Join(s.SeedDir, "snaps", "required20+comp1_22.comp"))
	c.Assert(err, IsNil)
	spuriousRev, err := s.StoreSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("required20"),
		"resource-name":     "comp1",
		"resource-sha3-384": strings.Repeat("B", 64),
		"resource-size":     fmt.Sprint(finfo.Size() + 4096),
		"resource-revision": "22",
		"snap-revision":     "11",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourceRevisionType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp1" {
				return []asserts.Assertion{spuriousRev}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, `resource comp1 size does not match size in resource revision: .*`)
}

func (s *seed20Suite) TestLoadMetaWithComponentsBadHash(c *C) {
	sysLabel := "20240805"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	finfo, err := os.Stat(filepath.Join(s.SeedDir, "snaps", "required20+comp1_22.comp"))
	c.Assert(err, IsNil)
	spuriousRev, err := s.StoreSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("required20"),
		"resource-name":     "comp1",
		"resource-sha3-384": strings.Repeat("B", 64),
		"resource-size":     fmt.Sprint(finfo.Size()),
		"resource-revision": "22",
		"snap-revision":     "11",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourceRevisionType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp1" {
				return []asserts.Assertion{spuriousRev}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, `cannot validate resource comp1, hash mismatch with snap-resource-revision`)
}

func (s *seed20Suite) TestLoadAssertionsUnbalancedResRevsAndPairs(c *C) {
	sysLabel := "20241031"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourcePairType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") {
				return nil
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, `system unexpectedly holds a different number of snap-snap-resource-revision than snap-resource-pair assertions`)
}

func (s *seed20Suite) TestLoadAssertionsNoMatchingPair(c *C) {
	sysLabel := "20241031"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	pairRev, err := s.StoreSigning.Sign(asserts.SnapResourcePairType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("required20"),
		"resource-name":     "comp1",
		"resource-revision": "101",
		"snap-revision":     "101",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourcePairType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp1" {
				return []asserts.Assertion{pairRev}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, fmt.Sprintf(`resource pair comp1 for %s does not match \(snap revision, resource revision\): \(11, 101\)`, s.AssertedSnapID("required20")))
}

func (s *seed20Suite) TestLoadAssertionsMultipleResRevForComp(c *C) {
	sysLabel := "20241031"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	resRev, err := s.StoreSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("required20"),
		"resource-name":     "comp1",
		"resource-sha3-384": strings.Repeat("B", 64),
		"resource-size":     "1024",
		"resource-revision": "101",
		"snap-revision":     "101",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	pairRev, err := s.StoreSigning.Sign(asserts.SnapResourcePairType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("required20"),
		"resource-name":     "comp1",
		"resource-revision": "101",
		"snap-revision":     "101",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourceRevisionType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp1" {
				return []asserts.Assertion{a, resRev, pairRev}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot have multiple resource revisions for the same component comp1 \(snap %s\)`, s.AssertedSnapID("required20")))
}

func (s *seed20Suite) TestLoadAssertionsNoMatchingResRevForResPair(c *C) {
	sysLabel := "20241031"
	sysDir := s.makeCore20SeedWithComps(c, sysLabel)

	spuriousRev, err := s.StoreSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           s.AssertedSnapID("core20"),
		"resource-name":     "comp1",
		"resource-sha3-384": strings.Repeat("B", 64),
		"resource-size":     "1024",
		"resource-revision": "101",
		"snap-revision":     "101",
		"developer-id":      "canonical",
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"),
		func(a asserts.Assertion) []asserts.Assertion {
			if a.Type() == asserts.SnapResourceRevisionType &&
				a.HeaderString("snap-id") == s.AssertedSnapID("required20") &&
				a.HeaderString("resource-name") == "comp1" {
				return []asserts.Assertion{spuriousRev}
			}
			return []asserts.Assertion{a}
		})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)
	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Check(err, ErrorMatches, fmt.Sprintf(`resource pair for comp1 \(%s\) does not have a matching resource revision`, s.AssertedSnapID("required20")))
}

func (s *seed20Suite) TestLoadMetaWithLocalComponents(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	localSnapPath := s.makeLocalSnap(c, "required20")
	localComp1Path := snaptest.MakeTestComponent(c, seedtest.SampleSnapYaml["required20+comp1"])
	localComp2Path := snaptest.MakeTestComponent(c, seedtest.SampleSnapYaml["required20+comp2"])

	sysLabel := "20240805"
	model := s.Brands.Model("my-brand", "my-model", map[string]interface{}{
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
				"type": "app",
				"components": map[string]interface{}{
					"comp1": "required",
					"comp2": "required",
				},
			},
		},
	})
	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)
	s.MakeSeedWithModel(c, sysLabel, model,
		[]*seedwriter.OptionsSnap{{Path: localSnapPath}},
		map[string][]string{"required20": {localComp1Path, localComp2Path}})

	seed20, err := seed.Open(s.SeedDir, sysLabel)
	c.Assert(err, IsNil)

	err = seed20.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          s.expectedPath("core20"),
			SideInfo:      &s.AssertedSnapInfo("core20").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		},
	})

	// check that PlaceInfo method works
	pi := essSnaps[0].PlaceInfo()
	c.Check(pi.Filename(), Equals, "snapd_1.snap")
	pi = essSnaps[1].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc-kernel_1.snap")
	pi = essSnaps[2].PlaceInfo()
	c.Check(pi.Filename(), Equals, "core20_1.snap")
	pi = essSnaps[3].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc_1.snap")

	runSnaps, err := seed20.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	req20sn := runSnaps[0]
	c.Check(req20sn.SnapName(), Equals, "required20")
	c.Check(len(req20sn.Components), Equals, 2)
	checked := make([]bool, 2)
	for _, comp := range req20sn.Components {
		switch comp.CompSideInfo.Component.ComponentName {
		case "comp1":
			c.Check(comp, DeepEquals, seed.Component{
				Path: filepath.Join(s.SeedDir, "systems", sysLabel,
					"snaps", "required20+comp1_1.0.comp"),
				CompSideInfo: &snap.ComponentSideInfo{
					Component: naming.NewComponentRef("required20", "comp1"),
				},
			})
			checked[0] = true
		case "comp2":
			c.Check(comp, DeepEquals, seed.Component{
				Path: filepath.Join(s.SeedDir, "systems", sysLabel,
					"snaps", "required20+comp2_2.0.comp"),
				CompSideInfo: &snap.ComponentSideInfo{
					Component: naming.NewComponentRef("required20", "comp2"),
				},
			})
			checked[1] = true
		}
	}
	c.Check(checked, DeepEquals, []bool{true, true})

	c.Check(seed20.NumSnaps(), Equals, 5)
}
