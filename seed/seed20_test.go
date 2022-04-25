// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/check.v1"
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

func (h *testSnapHandler) HandleUnassertedSnap(name, path string, _ timings.Measurer) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unasserted[name] = h.rel(path)
	return h.pathPrefix + path, nil
}

func (h *testSnapHandler) HandleAndDigestAssertedSnap(name, path string, essType snap.Type, snapRev *asserts.SnapRevision, deriveRev func(string, uint64) (snap.Revision, error), _ timings.Measurer) (string, string, uint64, error) {
	snapSHA3_384, sz, err := asserts.SnapFileSHA3_384(path)
	if err != nil {
		return "", "", 0, err
	}
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		revno := ""
		if snapRev != nil {
			revno = fmt.Sprintf("%d", snapRev.SnapRevision())
		} else {
			var rev snap.Revision
			rev, err = deriveRev(snapSHA3_384, sz)
			revno = rev.String()
		}
		h.asserted[name] = fmt.Sprintf("%s:%s:%s", h.rel(path), essType, revno)
	}()
	if essType != "gadget" {
		// XXX seed logic actually reads the gadget, leave it alone
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

	err = seed20.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot use gadget snap because its base "core18" is different from model base "core20"`)
}

func (s *seed20Suite) TestLoadMetaCore20(c *C) {
	s.makeSnap(c, "snapd", "")
	s.makeSnap(c, "core20", "")
	s.makeSnap(c, "pc-kernel=20", "")
	s.makeSnap(c, "pc=20", "")
	s.makeSnap(c, "required20", "developerid")

	s.AssertedSnapInfo("required20").EditedContact = "mailto:author@example.com"

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
		"snapd":     "snaps/snapd_1.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.snap:kernel:1",
		"core20":    "snaps/core20_1.snap:base:1",
		"pc":        "snaps/pc_1.snap:gadget:1",
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
	}

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

		for _, t := range tests {
			// test short-cut helper as well
			mod, essSnaps, betterTime, err := seed.ReadSystemEssentialAndBetterEarliestTime(s.SeedDir, sysLabel, t.onlyTypes, earliestTime, 0, s.perfTimings)
			c.Assert(err, IsNil)
			c.Check(mod.BrandID(), Equals, "my-brand")
			c.Check(mod.Model(), Equals, "my-model")
			c.Check(mod.Timestamp().Equal(modelTime), Equals, true)
			c.Check(essSnaps, HasLen, len(t.expected))
			c.Check(essSnaps, DeepEquals, t.expected)
			c.Check(betterTime.Equal(improvedTime), Equals, true, Commentf("%v expected: %v", betterTime, improvedTime))
		}
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

	_, _, _, err := seed.ReadSystemEssentialAndBetterEarliestTime(s.SeedDir, sysLabel, nil, time.Time{}, 3, s.perfTimings)
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
		"snapd":     "snaps/snapd_1.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.snap:kernel:1",
		"core20":    "snaps/core20_1.snap:base:1",
		"pc":        "snaps/pc_1.snap:gadget:1",
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
	h.pathPrefix = "saved"

	err = seed20.LoadMeta(seed.AllModes, h, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed20.UsesSnapdSnap(), Equals, true)

	essSnaps := seed20.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          "saved" + s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "latest/stable",
		}, {
			Path:          "saved" + s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "20",
		}, {
			Path:          "saved" + s.expectedPath("core20"),
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
			Path:     filepath.Join("saved", s.SeedDir, "systems", sysLabel, "snaps", "required20_1.0.snap"),
			SideInfo: &snap.SideInfo{RealName: "required20"},
			Required: true,
		},
	})

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.snap:kernel:1",
		"core20":    "snaps/core20_1.snap:base:1",
		"pc":        "snaps/pc_1.snap:gadget:1",
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

	s.AssertedSnapInfo("required20").EditedContact = "mailto:author@example.com"

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

	s.AssertedSnapInfo("required20").EditedContact = "mailto:author@example.com"

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
	handlers := map[string]seed.SnapHandler{
		"install": installH,
		"run":     runH,
		"recover": recoverH,
	}

	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 1, handlers)

	c.Check(installH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":    "snaps/pc-kernel_1.snap:kernel:1",
		"core20":       "snaps/core20_1.snap:base:1",
		"pc":           "snaps/pc_1.snap:gadget:1",
		"required20":   "snaps/required20_1.snap::1",
		"optional20-a": "snaps/optional20-a_1.snap::1",
		"optional20-b": "snaps/optional20-b_1.snap::1",
	})
	c.Check(runH.asserted, DeepEquals, map[string]string{
		"snapd":      "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":  "snaps/pc-kernel_1.snap:kernel:1",
		"core20":     "snaps/core20_1.snap:base:1",
		"pc":         "snaps/pc_1.snap:gadget:1",
		"required20": "snaps/required20_1.snap::1",
	})
	c.Check(recoverH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":    "snaps/pc-kernel_1.snap:kernel:1",
		"core20":       "snaps/core20_1.snap:base:1",
		"pc":           "snaps/pc_1.snap:gadget:1",
		"required20":   "snaps/required20_1.snap::1",
		"optional20-a": "snaps/optional20-a_1.snap::1",
	})
}

func (s *seed20Suite) testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c *C, parallelism int, handlers map[string]seed.SnapHandler) {
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
	handlers := map[string]seed.SnapHandler{
		"install": installH,
		"run":     runH,
		"recover": recoverH,
	}
	s.testLoadMetaCore20PreciseNotRunSnapsWithParallelism(c, 2, handlers)

	c.Check(installH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":    "snaps/pc-kernel_1.snap:kernel:1",
		"core20":       "snaps/core20_1.snap:base:1",
		"pc":           "snaps/pc_1.snap:gadget:1",
		"required20":   "snaps/required20_1.snap::1",
		"optional20-a": "snaps/optional20-a_1.snap::1",
		"optional20-b": "snaps/optional20-b_1.snap::1",
	})
	c.Check(runH.asserted, DeepEquals, map[string]string{
		"snapd":      "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":  "snaps/pc-kernel_1.snap:kernel:1",
		"core20":     "snaps/core20_1.snap:base:1",
		"pc":         "snaps/pc_1.snap:gadget:1",
		"required20": "snaps/required20_1.snap::1",
	})
	c.Check(recoverH.asserted, DeepEquals, map[string]string{
		"snapd":        "snaps/snapd_1.snap:snapd:1",
		"pc-kernel":    "snaps/pc-kernel_1.snap:kernel:1",
		"core20":       "snaps/core20_1.snap:base:1",
		"pc":           "snaps/pc_1.snap:gadget:1",
		"required20":   "snaps/required20_1.snap::1",
		"optional20-a": "snaps/optional20-a_1.snap::1",
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

	s.massageAssertions(c, filepath.Join(sysDir, "assertions", "snaps"), func(a asserts.Assertion) asserts.Assertion {
		if a.Type() == asserts.SnapRevisionType && a.HeaderString("snap-id") == s.AssertedSnapID("pc-kernel") {
			return wrongRev
		}
		return a
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
	// secured grade, no su assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelSecured, none, 0644, s.commitTo)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeSecuredAutoImportAssertion(c *C) {
	// secured grade, with su assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelSecured, valid, 0644, s.commitTo)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousNoAutoImportAssertion(c *C) {
	// dangerous grade, no su assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, none, 0644, s.commitTo)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionErrCommiter(c *C) {
	// dangerous grade with broken commiter
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, valid, 0644, func(b *asserts.Batch) error {
		return fmt.Errorf("nope")
	})
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionErrFilePerm(c *C) {
	// dangerous grade, su assertion with wrong file permissions
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, valid, 0222, s.commitTo)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousInvalidAutoImportAssertion(c *C) {
	// dangerous grade, invalid su assertion
	s.testLoadAutoImportAssertion(c, asserts.ModelDangerous, invalid, 0644, s.commitTo)
}

type suAsseertion int

const (
	none suAsseertion = iota
	valid
	invalid
)

func (s *seed20Suite) testLoadAutoImportAssertion(c *C, grade asserts.ModelGrade, sua suAsseertion, perm os.FileMode, commitTo func(b *asserts.Batch) error) {
	sysLabel := "20191018"
	seed20 := s.createMinimalSeed(c, string(grade), sysLabel)
	c.Assert(seed20, NotNil)
	c.Check(seed20.Model().Grade(), check.Equals, grade)

	// write test auto import assertion
	switch sua {
	case valid:
		s.writeValidAutoImportAssertion(c, sysLabel, perm)
	case invalid:
		s.writeInvalidAutoImportAssertion(c, sysLabel, perm)
	}

	// try to load auto import assertions
	err := seed20.LoadAutoImportAssertion(commitTo)

	c.Assert(err, IsNil)
	assertions, err := s.findAutoImportAssertion(seed20)
	c.Assert(err, NotNil)
	c.Check(err, check.ErrorMatches, "No system user assertion found")
	c.Assert(assertions, IsNil)
}

func (s *seed20Suite) TestLoadAutoImportAssertionGradeDangerousAutoImportAssertionHappy(c *C) {
	sysLabel := "20191018"
	seed20 := s.createMinimalSeed(c, "dangerous", sysLabel)
	c.Assert(seed20, NotNil)
	c.Check(seed20.Model().Grade(), check.Equals, asserts.ModelDangerous)

	s.writeValidAutoImportAssertion(c, sysLabel, 0644)

	// try to load auto import assertions
	err := seed20.LoadAutoImportAssertion(s.commitTo)
	c.Assert(err, IsNil)
	c.Assert(err, IsNil)
	assertions, err := s.findAutoImportAssertion(seed20)
	c.Assert(err, IsNil)
	// validate it's our assertion
	c.Check(len(assertions), check.Equals, 1)
	su := assertions[0].(*asserts.SystemUser)
	c.Check(su.Username(), check.Equals, "guy")
	c.Check(su.Email(), check.Equals, "foo@bar.com")
	c.Check(su.Name(), check.Equals, "Boring Guy")
	c.Check(su.AuthorityID(), check.Equals, "my-brand")
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

var goodUser = map[string]interface{}{
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foo@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model", "other-model"},
	"name":         "Boring Guy",
	"username":     "guy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func (s *seed20Suite) writeValidAutoImportAssertion(c *C, sysLabel string, perm os.FileMode) {
	systemUsers := []map[string]interface{}{goodUser}
	// write system user asseerion to system seed root
	autoImportAssert := filepath.Join(s.SeedDir, "systems", sysLabel, "auto-import.assert")
	f, err := os.OpenFile(autoImportAssert, os.O_CREATE|os.O_WRONLY, perm)
	c.Assert(err, IsNil)
	defer f.Close()
	enc := asserts.NewEncoder(f)
	c.Assert(enc, NotNil)

	for _, suMap := range systemUsers {
		su, err := s.Brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, IsNil)
		su = su.(*asserts.SystemUser)
		err = enc.Encode(su)
		c.Assert(err, IsNil)
	}
}

func (s *seed20Suite) writeInvalidAutoImportAssertion(c *C, sysLabel string, perm os.FileMode) {
	autoImportAssert := filepath.Join(s.SeedDir, "systems", sysLabel, "auto-import.assert")
	// write random data
	randomness := make([]byte, 512)
	rand.Read(randomness)
	err := ioutil.WriteFile(autoImportAssert, randomness, perm)
	c.Assert(err, IsNil)
}

// findAutoImportAssertion returns found systemUser assertion
func (s *seed20Suite) findAutoImportAssertion(seed20 seed.Seed) ([]asserts.Assertion, error) {
	assertions, err := s.db.FindMany(asserts.SystemUserType, map[string]string{
		"brand-id": seed20.Model().BrandID(),
	})

	if asserts.IsNotFound(err) {
		return nil, fmt.Errorf("No system user assertion found")
	}
	if err != nil {
		return nil, err
	}

	return assertions, nil
}
