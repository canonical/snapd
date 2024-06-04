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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func Test(t *testing.T) { TestingT(t) }

type seed16Suite struct {
	testutil.BaseTest

	*seedtest.TestingSeed16
	devAcct *asserts.Account

	seed16 seed.Seed

	db *asserts.Database

	perfTimings timings.Measurer
}

var _ = Suite(&seed16Suite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *seed16Suite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	s.SeedDir = c.MkDir()

	s.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")
	assertstest.AddMany(s.StoreSigning, s.devAcct)

	seed16, err := seed.Open(s.SeedDir, "")
	c.Assert(err, IsNil)
	s.seed16 = seed16

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db

	s.perfTimings = timings.New(nil)
}

func (s *seed16Suite) commitTo(b *asserts.Batch) error {
	return b.CommitTo(s.db, nil)
}

func (s *seed16Suite) TestLoadAssertionsNoAssertions(c *C) {
	c.Check(s.seed16.LoadAssertions(s.db, s.commitTo), Equals, seed.ErrNoAssertions)
}

func (s *seed16Suite) TestLoadAssertionsNoModelAssertion(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	c.Check(s.seed16.LoadAssertions(s.db, s.commitTo), ErrorMatches, "seed must have a model assertion")
}

func (s *seed16Suite) TestLoadAssertionsTwoModelAssertionsError(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", modelChain...)
	modelChain = s.MakeModelAssertionChain("my-brand", "my-model-2", headers)
	s.WriteAssertions("model2.asserts", modelChain...)

	c.Check(s.seed16.LoadAssertions(s.db, s.commitTo), ErrorMatches, "cannot have multiple model assertions in seed")
}

func (s *seed16Suite) TestLoadAssertionsConsistencyError(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	// write out only the model assertion
	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	model := s.Brands.Model("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", model)

	c.Check(s.seed16.LoadAssertions(s.db, s.commitTo), ErrorMatches, "cannot resolve prerequisite assertion: account-key .*")
}

func (s *seed16Suite) TestLoadAssertionsModelHappy(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", modelChain...)

	err = s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	model := s.seed16.Model()
	c.Check(model.Model(), Equals, "my-model")

	_, err = s.db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	})
	c.Assert(err, IsNil)
}

func (s *seed16Suite) TestLoadAssertionsModelTempDBHappy(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", modelChain...)

	err = s.seed16.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	model := s.seed16.Model()
	c.Check(model.Model(), Equals, "my-model")

	brand, err := s.seed16.Brand()
	c.Assert(err, IsNil)
	c.Check(brand.AccountID(), Equals, "my-brand")
	c.Check(brand.DisplayName(), Equals, "My-brand")
}

func (s *seed16Suite) TestLoadMetaNoMeta(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", modelChain...)

	err = s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, Equals, seed.ErrNoMeta)
}

func (s *seed16Suite) TestLoadMetaInvalidSeedYaml(c *C) {
	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	}
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", headers)
	s.WriteAssertions("model.asserts", modelChain...)

	err = s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	// create a seed.yaml
	content, err := yaml.Marshal(map[string]interface{}{
		"snaps": []*seed.InternalSnap16{{
			Name:    "core",
			Channel: "track/not-a-risk",
		}},
	})
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.SeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Check(err, ErrorMatches, `cannot read seed yaml: invalid risk in channel name: track/not-a-risk`)
}

var snapYaml = seedtest.MergeSampleSnapYaml(seedtest.SampleSnapYaml, map[string]string{
	"private-snap": `name: private-snap
base: core18
version: 1.0
`,
	"contactable-snap": `name: contactable-snap
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
})

const pcGadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

var pcGadgetFiles = [][]string{
	{"meta/gadget.yaml", pcGadgetYaml},
}

var snapFiles = map[string][][]string{
	"pc":    pcGadgetFiles,
	"pc=18": pcGadgetFiles,
}

var snapPublishers = map[string]string{
	"required": "developerid",
}

var (
	coreSeed = &seed.InternalSnap16{
		Name:    "core",
		Channel: "stable",
	}
	kernelSeed = &seed.InternalSnap16{
		Name:    "pc-kernel",
		Channel: "stable",
	}
	gadgetSeed = &seed.InternalSnap16{
		Name:    "pc",
		Channel: "stable",
	}
	requiredSeed = &seed.InternalSnap16{
		Name:    "required",
		Channel: "stable",
	}
	// Core 18
	snapdSeed = &seed.InternalSnap16{
		Name:    "snapd",
		Channel: "stable",
	}
	core18Seed = &seed.InternalSnap16{
		Name:    "core18",
		Channel: "stable",
	}
	kernel18Seed = &seed.InternalSnap16{
		Name:    "pc-kernel",
		Channel: "18",
	}
	gadget18Seed = &seed.InternalSnap16{
		Name:    "pc",
		Channel: "18",
	}
	required18Seed = &seed.InternalSnap16{
		Name:    "required18",
		Channel: "stable",
	}
	classicSnapSeed = &seed.InternalSnap16{
		Name:    "classic-snap",
		Channel: "stable",
		Classic: true,
	}
	classicGadgetSeed = &seed.InternalSnap16{
		Name:    "classic-gadget",
		Channel: "stable",
	}
	classicGadget18Seed = &seed.InternalSnap16{
		Name:    "classic-gadget18",
		Channel: "stable",
	}
	privateSnapSeed = &seed.InternalSnap16{
		Name:    "private-snap",
		Channel: "stable",
		Private: true,
	}
	contactableSnapSeed = &seed.InternalSnap16{
		Name:    "contactable-snap",
		Channel: "stable",
		Contact: "mailto:author@example.com",
	}
)

func (s *seed16Suite) makeSeed(c *C, modelHeaders map[string]interface{}, seedSnaps ...*seed.InternalSnap16) []*seed.InternalSnap16 {
	coreHeaders := map[string]interface{}{
		"architecture": "amd64",
	}

	if _, ok := modelHeaders["classic"]; !ok {
		coreHeaders["kernel"] = "pc-kernel"
		coreHeaders["gadget"] = "pc"
	}

	err := os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", coreHeaders, modelHeaders)
	s.WriteAssertions("model.asserts", modelChain...)

	err = os.Mkdir(s.SnapsDir(), 0755)
	c.Assert(err, IsNil)

	var completeSeedSnaps []*seed.InternalSnap16
	for _, seedSnap := range seedSnaps {
		completeSeedSnap := *seedSnap
		var snapFname string
		if seedSnap.Unasserted {
			mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml[seedSnap.Name], snapFiles[seedSnap.Name])
			snapFname = filepath.Base(mockSnapFile)
			err := os.Rename(mockSnapFile, filepath.Join(s.SeedDir, "snaps", snapFname))
			c.Assert(err, IsNil)
		} else {
			publisher := snapPublishers[seedSnap.Name]
			if publisher == "" {
				publisher = "canonical"
			}
			whichYaml := seedSnap.Name
			if seedSnap.Channel != "stable" {
				whichYaml = whichYaml + "=" + seedSnap.Channel
			}
			fname, decl, rev := s.MakeAssertedSnap(c, snapYaml[whichYaml], snapFiles[whichYaml], snap.R(1), publisher)
			acct, err := s.StoreSigning.Find(asserts.AccountType, map[string]string{"account-id": publisher})
			c.Assert(err, IsNil)
			s.WriteAssertions(fmt.Sprintf("%s.asserts", seedSnap.Name), rev, decl, acct)
			snapFname = fname
		}
		completeSeedSnap.File = snapFname
		completeSeedSnaps = append(completeSeedSnaps, &completeSeedSnap)
	}

	s.writeSeed(c, completeSeedSnaps)

	return completeSeedSnaps
}

func (s *seed16Suite) writeSeed(c *C, seedSnaps []*seed.InternalSnap16) {
	// create a seed.yaml
	content, err := yaml.Marshal(map[string]interface{}{
		"snaps": seedSnaps,
	})
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.SeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)
}

func (s *seed16Suite) expectedPath(snapName string) string {
	return filepath.Join(s.SeedDir, "snaps", filepath.Base(s.AssertedSnap(snapName)))
}

func (s *seed16Suite) TestLoadMetaCore16Minimal(c *C) {
	s.makeSeed(c, nil, coreSeed, kernelSeed, gadgetSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, false)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 3)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("core"),
			SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
			EssentialType: snap.TypeOS,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaCore16(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"required-snaps": []interface{}{"required"},
	}, coreSeed, kernelSeed, gadgetSeed, requiredSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 3)

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)

	// check that PlaceInfo method works
	pi := essSnaps[0].PlaceInfo()
	c.Check(pi.Filename(), Equals, "core_1.snap")
	pi = essSnaps[1].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc-kernel_1.snap")
	pi = essSnaps[2].PlaceInfo()
	c.Check(pi.Filename(), Equals, "pc_1.snap")

	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required"),
			SideInfo: &s.AssertedSnapInfo("required").SideInfo,
			Required: true,
			Channel:  "stable",
		},
	})
}

func (s *seed16Suite) TestLoadMetaCore18Minimal(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"base":   "core18",
		"kernel": "pc-kernel=18",
		"gadget": "pc=18",
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaCore18(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, requiredSeed, coreSeed, required18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 3)

	// these are not sorted by type, firstboot will do that
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required"),
			SideInfo: &s.AssertedSnapInfo("required").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("core"),
			SideInfo: &s.AssertedSnapInfo("core").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("required18"),
			SideInfo: &s.AssertedSnapInfo("required18").SideInfo,
			Required: true,
			Channel:  "stable",
		},
	})
}

func (s *seed16Suite) TestLoadMetaClassicNothing(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic": "true",
	})

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, false)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 0)

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaClassicCore(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic": "true",
	}, coreSeed, classicSnapSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, false)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 1)
	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("core"),
			SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
			EssentialType: snap.TypeOS,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		},
	})

	// classic-snap is not required, just an extra snap
	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("classic-snap"),
			SideInfo: &s.AssertedSnapInfo("classic-snap").SideInfo,
			Channel:  "stable",
			Classic:  true,
		},
	})
}

func (s *seed16Suite) TestLoadMetaClassicCoreWithGadget(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic": "true",
		"gadget":  "classic-gadget",
	}, coreSeed, classicGadgetSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, false)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 2)
	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("core"),
			SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
			EssentialType: snap.TypeOS,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		},
		{
			Path:          s.expectedPath("classic-gadget"),
			SideInfo:      &s.AssertedSnapInfo("classic-gadget").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaClassicSnapd(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic":        "true",
		"required-snaps": []interface{}{"core18", "required18"},
	}, snapdSeed, core18Seed, required18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 1)
	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 2)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("core18"),
			SideInfo: &s.AssertedSnapInfo("core18").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("required18"),
			SideInfo: &s.AssertedSnapInfo("required18").SideInfo,
			Required: true,
			Channel:  "stable",
		},
	})
}

func (s *seed16Suite) TestLoadMetaClassicSnapdWithGadget(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic": "true",
		"gadget":  "classic-gadget",
	}, snapdSeed, classicGadgetSeed, coreSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 3)
	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		}, {
			Path:          s.expectedPath("classic-gadget"),
			SideInfo:      &s.AssertedSnapInfo("classic-gadget").SideInfo,
			EssentialType: snap.TypeGadget,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		}, {
			Path:          s.expectedPath("core"),
			SideInfo:      &s.AssertedSnapInfo("core").SideInfo,
			EssentialType: snap.TypeOS,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaClassicSnapdWithGadget18(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"classic":        "true",
		"gadget":         "classic-gadget18",
		"required-snaps": []interface{}{"core", "required"},
	}, snapdSeed, coreSeed, requiredSeed, classicGadget18Seed, core18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 3)
	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		}, {
			Path:          s.expectedPath("classic-gadget18"),
			SideInfo:      &s.AssertedSnapInfo("classic-gadget18").SideInfo,
			EssentialType: snap.TypeGadget,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,

			Essential: true,
			Required:  true,
			Channel:   "stable",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 2)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("core"),
			SideInfo: &s.AssertedSnapInfo("core").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("required"),
			SideInfo: &s.AssertedSnapInfo("required").SideInfo,
			Required: true,
			Channel:  "stable",
		},
	})
}

func (s *seed16Suite) TestLoadMetaCore18Local(c *C) {
	localRequired18Seed := &seed.InternalSnap16{
		Name:       "required18",
		Unasserted: true,
		DevMode:    true,
	}
	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, localRequired18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)

	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join(s.SeedDir, "snaps", "required18_1.0_all.snap"),
			SideInfo: &snap.SideInfo{RealName: "required18"},
			Required: true,
			DevMode:  true,
		},
	})
}

func (s *seed16Suite) TestLoadMetaCore18SnapHandler(c *C) {
	localRequired18Seed := &seed.InternalSnap16{
		Name:       "required18",
		Unasserted: true,
		DevMode:    true,
	}
	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, localRequired18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	h := newTestSnapHandler(s.SeedDir)

	err = s.seed16.LoadMeta(seed.AllModes, h, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)

	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join(s.SeedDir, "snaps", "required18_1.0_all.snap"),
			SideInfo: &snap.SideInfo{RealName: "required18"},
			Required: true,
			DevMode:  true,
		},
	})

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.0_all.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.0_all.snap:kernel:1",
		"core18":    "snaps/core18_1.0_all.snap:base:1",
		"pc":        "snaps/pc_1.0_all.snap:gadget:1",
	})
	c.Check(h.unasserted, DeepEquals, map[string]string{
		"required18": "snaps/required18_1.0_all.snap",
	})
}

func (s *seed16Suite) TestLoadMetaCore18SnapHandlerChangePath(c *C) {
	localRequired18Seed := &seed.InternalSnap16{
		Name:       "required18",
		Unasserted: true,
		DevMode:    true,
	}
	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, localRequired18Seed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	h := newTestSnapHandler(s.SeedDir)
	h.pathPrefix = "saved"

	err = s.seed16.LoadMeta(seed.AllModes, h, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          "saved" + s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          "saved" + s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          "saved" + s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 1)

	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     filepath.Join("saved", s.SeedDir, "snaps", "required18_1.0_all.snap"),
			SideInfo: &snap.SideInfo{RealName: "required18"},
			Required: true,
			DevMode:  true,
		},
	})

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.0_all.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.0_all.snap:kernel:1",
		"core18":    "snaps/core18_1.0_all.snap:base:1",
		"pc":        "snaps/pc_1.0_all.snap:gadget:1",
	})
	c.Check(h.unasserted, DeepEquals, map[string]string{
		"required18": "snaps/required18_1.0_all.snap",
	})
}

func (s *seed16Suite) TestLoadMetaCore18StoreInfo(c *C) {
	s.makeSeed(c, map[string]interface{}{
		"base":   "core18",
		"kernel": "pc-kernel=18",
		"gadget": "pc=18",
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, privateSnapSeed, contactableSnapSeed)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 2)

	privateSnapSideInfo := s.AssertedSnapInfo("private-snap").SideInfo
	privateSnapSideInfo.Private = true
	contactableSnapSideInfo := s.AssertedSnapInfo("contactable-snap").SideInfo
	contactableSnapSideInfo.LegacyEditedContact = "mailto:author@example.com"

	// these are not sorted by type, firstboot will do that
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("private-snap"),
			SideInfo: &privateSnapSideInfo,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("contactable-snap"),
			SideInfo: &contactableSnapSideInfo,
			Channel:  "stable",
		},
	})
}

func (s *seed16Suite) TestLoadMetaCore18EnforcePinnedTracks(c *C) {
	seedSnaps := s.makeSeed(c, map[string]interface{}{
		"base":   "core18",
		"kernel": "pc-kernel=18",
		"gadget": "pc=18",
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed)

	// tweak channels
	for _, sn := range seedSnaps {
		switch sn.Name {
		case "pc":
			sn.Channel = "edge"
		case "pc-kernel":
			sn.Channel = "latest/candidate"
		}
	}
	s.writeSeed(c, seedSnaps)

	err := s.seed16.LoadAssertions(s.db, s.commitTo)
	c.Assert(err, IsNil)

	err = s.seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(s.seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := s.seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, 4)

	c.Check(essSnaps, DeepEquals, []*seed.Snap{
		{
			Path:          s.expectedPath("snapd"),
			SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
			EssentialType: snap.TypeSnapd,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("core18"),
			SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
			EssentialType: snap.TypeBase,
			Essential:     true,
			Required:      true,
			Channel:       "stable",
		}, {
			Path:          s.expectedPath("pc-kernel"),
			SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
			EssentialType: snap.TypeKernel,
			Essential:     true,
			Required:      true,
			Channel:       "18",
		}, {
			Path:          s.expectedPath("pc"),
			SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
			EssentialType: snap.TypeGadget,
			Essential:     true,
			Required:      true,
			Channel:       "18/edge",
		},
	})

	runSnaps, err := s.seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)
}

func (s *seed16Suite) TestLoadMetaBrokenSeed(c *C) {
	seedSnap16s := s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, required18Seed)

	otherSnapFile := snaptest.MakeTestSnapWithFiles(c, `name: other
version: other`, nil)
	otherFname := filepath.Base(otherSnapFile)
	err := os.Rename(otherSnapFile, filepath.Join(s.SeedDir, "snaps", otherFname))
	c.Assert(err, IsNil)

	const otherBaseGadget = `name: pc
type: gadget
base: other-base
version: other-base
`
	otherBaseGadgetFname, obgDecl, obgRev := s.MakeAssertedSnap(c, otherBaseGadget, snapFiles["pc"], snap.R(3), "canonical")
	s.WriteAssertions("other-gadget.asserts", obgDecl, obgRev)

	omit := func(which int) func([]*seed.InternalSnap16) []*seed.InternalSnap16 {
		return func(snaps []*seed.InternalSnap16) []*seed.InternalSnap16 {
			broken := make([]*seed.InternalSnap16, 0, len(snaps)-1)
			for i, sn := range snaps {
				if i == which {
					continue
				}
				broken = append(broken, sn)
			}
			return broken
		}
	}
	replaceFile := func(snapName, fname string) func([]*seed.InternalSnap16) []*seed.InternalSnap16 {
		return func(snaps []*seed.InternalSnap16) []*seed.InternalSnap16 {
			for i := range snaps {
				if snaps[i].Name != snapName {
					continue
				}
				sn := *snaps[i]
				sn.File = fname
				snaps[i] = &sn
			}
			return snaps
		}
	}

	tests := []struct {
		breakSeed func([]*seed.InternalSnap16) []*seed.InternalSnap16
		err       string
	}{
		{omit(0), `essential snap "snapd" required by the model is missing in the seed`},
		{omit(1), `essential snap "core18" required by the model is missing in the seed`},
		{omit(2), `essential snap "pc-kernel" required by the model is missing in the seed`},
		{omit(3), `essential snap "pc" required by the model is missing in the seed`},
		// omitting "required18" currently doesn't error in any way
		{replaceFile("core18", otherFname), `cannot find signatures with metadata for snap "core18".*`},
		{replaceFile("required18", otherFname), `cannot find signatures with metadata for snap "required18".*`},
		{replaceFile("core18", "not-existent"), `cannot compute snap .* digest: .*`},
		{replaceFile("pc", otherBaseGadgetFname), `cannot use gadget snap because its base "other-base" is different from model base "core18"`},
	}

	for _, t := range tests {
		seed16, err := seed.Open(s.SeedDir, "")
		c.Assert(err, IsNil)

		err = seed16.LoadAssertions(s.db, s.commitTo)
		c.Assert(err, IsNil)

		testSeedSnap16s := make([]*seed.InternalSnap16, 5)
		copy(testSeedSnap16s, seedSnap16s)

		testSeedSnap16s = t.breakSeed(testSeedSnap16s)
		s.writeSeed(c, testSeedSnap16s)

		c.Check(seed16.LoadMeta(seed.AllModes, nil, s.perfTimings), ErrorMatches, t.err)
	}
}

func (s *seed16Suite) TestLoadEssentialMetaCore18(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, requiredSeed, coreSeed, required18Seed)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	core18Snap := &seed.Snap{
		Path:          s.expectedPath("core18"),
		SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}
	coreSnap := &seed.Snap{
		Path: s.expectedPath("core"),
	}
	requiredSnap := &seed.Snap{
		Path: s.expectedPath("required"),
	}
	required18Snap := &seed.Snap{
		Path: s.expectedPath("required18"),
	}

	all := []*seed.Snap{snapdSnap, pcKernelSnap, pcSnap, core18Snap, coreSnap, requiredSnap, required18Snap}

	tests := []struct {
		onlyTypes []snap.Type
		expected  []*seed.Snap
	}{
		{[]snap.Type{snap.TypeSnapd}, []*seed.Snap{snapdSnap}},
		{[]snap.Type{snap.TypeKernel}, []*seed.Snap{pcKernelSnap}},
		{[]snap.Type{snap.TypeBase}, []*seed.Snap{core18Snap}},
		{[]snap.Type{snap.TypeGadget}, []*seed.Snap{pcSnap}},
		// the order in essentialTypes is not relevant
		{[]snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase}, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap}},
		{[]snap.Type{snap.TypeGadget, snap.TypeKernel}, []*seed.Snap{pcKernelSnap, pcSnap}},
		// degenerate case
		{[]snap.Type{}, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap, pcSnap}},
		{nil, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap, pcSnap}},
	}

	for _, t := range tests {
		// hide the non-requested snaps to make sure they are not
		// accessed
		var unhide func()
		if len(t.onlyTypes) != 0 {
			unhide = hideSnaps(c, all, t.onlyTypes)
		}

		seed16, err := seed.Open(s.SeedDir, "")
		c.Assert(err, IsNil)

		err = seed16.LoadAssertions(nil, nil)
		c.Assert(err, IsNil)

		err = seed16.LoadEssentialMeta(t.onlyTypes, s.perfTimings)
		c.Assert(err, IsNil)

		c.Check(seed16.UsesSnapdSnap(), Equals, true)

		essSnaps := seed16.EssentialSnaps()
		c.Check(essSnaps, HasLen, len(t.expected))

		c.Check(essSnaps, DeepEquals, t.expected)

		runSnaps, err := seed16.ModeSnaps("run")
		c.Assert(err, IsNil)
		c.Check(runSnaps, HasLen, 0)

		if unhide != nil {
			unhide()
		}
	}
}

func (s *seed16Suite) TestLoadEssentialMetaWithSnapHandlerCore18(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, requiredSeed, coreSeed, required18Seed)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	core18Snap := &seed.Snap{
		Path:          s.expectedPath("core18"),
		SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}

	expected := []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap, pcSnap}

	seed16, err := seed.Open(s.SeedDir, "")
	c.Assert(err, IsNil)

	err = seed16.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	h := newTestSnapHandler(s.SeedDir)

	err = seed16.LoadEssentialMetaWithSnapHandler(nil, h, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := seed16.EssentialSnaps()
	c.Check(essSnaps, HasLen, len(expected))
	c.Check(essSnaps, DeepEquals, expected)

	c.Check(h.asserted, DeepEquals, map[string]string{
		"snapd":     "snaps/snapd_1.0_all.snap:snapd:1",
		"pc-kernel": "snaps/pc-kernel_1.0_all.snap:kernel:1",
		"core18":    "snaps/core18_1.0_all.snap:base:1",
		"pc":        "snaps/pc_1.0_all.snap:gadget:1",
	})
}

func (s *seed16Suite) TestLoadEssentialAndMetaCore18(c *C) {
	r := seed.MockTrusted(s.StoreSigning.Trusted)
	defer r()

	s.makeSeed(c, map[string]interface{}{
		"base":           "core18",
		"kernel":         "pc-kernel=18",
		"gadget":         "pc=18",
		"required-snaps": []interface{}{"core", "required", "required18"},
	}, snapdSeed, core18Seed, kernel18Seed, gadget18Seed, requiredSeed, coreSeed, required18Seed)

	snapdSnap := &seed.Snap{
		Path:          s.expectedPath("snapd"),
		SideInfo:      &s.AssertedSnapInfo("snapd").SideInfo,
		EssentialType: snap.TypeSnapd,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	core18Snap := &seed.Snap{
		Path:          s.expectedPath("core18"),
		SideInfo:      &s.AssertedSnapInfo("core18").SideInfo,
		EssentialType: snap.TypeBase,
		Essential:     true,
		Required:      true,
		Channel:       "stable",
	}
	pcKernelSnap := &seed.Snap{
		Path:          s.expectedPath("pc-kernel"),
		SideInfo:      &s.AssertedSnapInfo("pc-kernel").SideInfo,
		EssentialType: snap.TypeKernel,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}
	pcSnap := &seed.Snap{
		Path:          s.expectedPath("pc"),
		SideInfo:      &s.AssertedSnapInfo("pc").SideInfo,
		EssentialType: snap.TypeGadget,
		Essential:     true,
		Required:      true,
		Channel:       "18",
	}

	seed16, err := seed.Open(s.SeedDir, "")
	c.Assert(err, IsNil)

	err = seed16.LoadAssertions(nil, nil)
	c.Assert(err, IsNil)

	err = seed16.LoadEssentialMeta([]snap.Type{snap.TypeGadget}, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed16.UsesSnapdSnap(), Equals, true)

	essSnaps := seed16.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{pcSnap})

	err = seed16.LoadEssentialMeta([]snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase, snap.TypeGadget}, s.perfTimings)
	c.Assert(err, IsNil)

	essSnaps = seed16.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap, pcSnap})

	runSnaps, err := seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 0)

	// caching in place
	hideSnaps(c, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap}, nil)

	err = seed16.LoadMeta(seed.AllModes, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(seed16.UsesSnapdSnap(), Equals, true)

	essSnaps = seed16.EssentialSnaps()
	c.Check(essSnaps, DeepEquals, []*seed.Snap{snapdSnap, core18Snap, pcKernelSnap, pcSnap})

	runSnaps, err = seed16.ModeSnaps("run")
	c.Assert(err, IsNil)
	c.Check(runSnaps, HasLen, 3)
	c.Check(runSnaps, DeepEquals, []*seed.Snap{
		{
			Path:     s.expectedPath("required"),
			SideInfo: &s.AssertedSnapInfo("required").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("core"),
			SideInfo: &s.AssertedSnapInfo("core").SideInfo,
			Required: true,
			Channel:  "stable",
		}, {
			Path:     s.expectedPath("required18"),
			SideInfo: &s.AssertedSnapInfo("required18").SideInfo,
			Required: true,
			Channel:  "stable",
		},
	})
}
