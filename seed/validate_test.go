// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type validateSuite struct {
	testutil.BaseTest

	*seedtest.TestingSeed16
}

var _ = Suite(&validateSuite{})

var coreYaml = `name: core
version: 1.0
type: os
`

const snapdYaml = `name: snapd
version: 1.0
type: snapd
`

const packageCore18 = `
name: core18
version: 18.04
type: base
`

func (s *validateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(builtin.SanitizePlugsSlots))

	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	s.SeedDir = c.MkDir()

	s.AddCleanup(seed.MockTrusted(s.StoreSigning.Trusted))

	err := os.MkdirAll(s.SnapsDir(), 0755)
	c.Assert(err, IsNil)
	err = os.Mkdir(s.AssertsDir(), 0755)
	c.Assert(err, IsNil)

	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", map[string]interface{}{
		"classic": "true",
	})
	s.WriteAssertions("model.asserts", modelChain...)
}

func (s *validateSuite) makeSnapInSeed(c *C, snapYaml string) {
	publisher := "canonical"
	fname, decl, rev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(1), publisher)
	err := os.Rename(filepath.Join(s.SnapsDir(), fname), filepath.Join(s.SnapsDir(), fmt.Sprintf("%s_%s.snap", decl.SnapName(), snap.R(1))))
	c.Assert(err, IsNil)

	acct, err := s.StoreSigning.Find(asserts.AccountType, map[string]string{"account-id": publisher})
	c.Assert(err, IsNil)
	s.WriteAssertions(fmt.Sprintf("%s.asserts", decl.SnapName()), rev, decl, acct)
}

func (s *validateSuite) makeSeedYaml(c *C, seedYaml string) string {
	tmpf := filepath.Join(s.SeedDir, "seed.yaml")
	err := os.WriteFile(tmpf, []byte(seedYaml), 0644)
	c.Assert(err, IsNil)
	return tmpf
}

func (s *validateSuite) TestValidateFromYamlSnapHappy(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	s.makeSnapInSeed(c, `name: gtk-common-themes
version: 19.04`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   channel: stable
   file: core_1.snap
 - name: gtk-common-themes
   channel: stable/ubuntu-19.04
   file: gtk-common-themes_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, IsNil)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingBase(c *C) {
	s.makeSnapInSeed(c, `name: need-base
base: some-base
version: 1.0`)
	s.makeSnapInSeed(c, coreYaml)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: need-base
   file: need-base_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot use snap "need-base": base "some-base" is missing`)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingDefaultProvider(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	s.makeSnapInSeed(c, `name: need-df
version: 1.0
plugs:
 gtk-3-themes:
  interface: content
  default-provider: gtk-common-themes
  target: $SNAP/themes
`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: need-df
   file: need-df_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "gtk-3-themes" is missing`)
}

func (s *validateSuite) TestValidateFromYamlSnapSnapdHappy(c *C) {
	s.makeSnapInSeed(c, snapdYaml)
	s.makeSnapInSeed(c, packageCore18)
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0
base: core18
`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: snapd
   file: snapd_1.snap
 - name: some-snap
   file: some-snap_1.snap
 - name: core18
   file: core18_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, IsNil)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingCore(c *C) {
	s.makeSnapInSeed(c, snapdYaml)
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: snapd
   file: snapd_1.snap
 - name: some-snap
   file: some-snap_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot use snap "some-snap": required snap "core" missing`)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingSnapdOrCore(c *C) {
	s.makeSnapInSeed(c, packageCore18)
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0
base: core18`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: some-snap
   file: some-snap_1.snap
 - name: core18
   file: core18_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - essential snap core or snapd must be part of the seed`)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingSnapd(c *C) {
	modelChain := s.MakeModelAssertionChain("my-brand", "my-model", map[string]interface{}{
		"classic":        "true",
		"required-snaps": []interface{}{"snapd"},
	})
	s.WriteAssertions("model.asserts", modelChain...)

	s.makeSnapInSeed(c, packageCore18)
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0
base: core18`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: some-snap
   file: some-snap_1.snap
 - name: core18
   file: core18_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - essential snap "snapd" required by the model is missing in the seed`)
}

func (s *validateSuite) makeBrokenSnap(c *C, snapYaml string) (snapPath string) {
	snapBuildDir := c.MkDir()
	metaSnapYaml := filepath.Join(snapBuildDir, "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(metaSnapYaml), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(metaSnapYaml, []byte(snapYaml), 0644)
	c.Assert(err, IsNil)

	// need to build the snap "manually" pack.Snap() will do validation
	snapPath = filepath.Join(c.MkDir(), "broken.snap")
	d := squashfs.New(snapPath)
	err = d.Build(snapBuildDir, nil)
	c.Assert(err, IsNil)

	return snapPath
}

func (s *validateSuite) TestValidateFromYamlSnapSnapInvalid(c *C) {
	s.makeSnapInSeed(c, coreYaml)

	// "version" is missing in this yaml
	snapYaml := `name: some-snap-invalid-yaml`
	snapPath := s.makeBrokenSnap(c, snapYaml)

	// put the broken snap in place
	dst := filepath.Join(s.SnapsDir(), "some-snap-invalid-yaml_1.snap")
	err := os.Rename(snapPath, dst)
	c.Assert(err, IsNil)

	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: some-snap-invalid-yaml
   unasserted: true
   file: some-snap-invalid-yaml_1.snap
`)

	err = seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot use snap "/.*/snaps/some-snap-invalid-yaml_1.snap": invalid snap version: cannot be empty`)
}

func (s *validateSuite) TestValidateFromYamlSnapMultipleErrors(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	s.makeSnapInSeed(c, `name: need-df
version: 1.0
plugs:
 gtk-3-themes:
  interface: content
  default-provider: gtk-common-themes
  target: $SNAP/themes
`)

	// "version" is missing in this yaml
	snapYaml := `name: some-snap-invalid-yaml`
	snapPath := s.makeBrokenSnap(c, snapYaml)

	// put the broken snap in place
	dst := filepath.Join(s.SnapsDir(), "some-snap-invalid-yaml_1.snap")
	err := os.Rename(snapPath, dst)
	c.Assert(err, IsNil)

	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: need-df
   file: need-df_1.snap
 - name: some-snap-invalid-yaml
   unasserted: true
   file: some-snap-invalid-yaml_1.snap
`)

	err = seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot use snap "/.*/snaps/some-snap-invalid-yaml_1.snap": invalid snap version: cannot be empty
 - cannot use snap "need-df": default provider "gtk-common-themes" or any alternative provider for content "gtk-3-themes" is missing`)
}

func (s *validateSuite) TestValidateFromYamlSnapSnapMissing(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: some-snap
   file: some-snap_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot compute snap "/.*/snaps/some-snap_1.snap" digest:.* no such file or directory`)
}

func (s *validateSuite) TestValidateFromYamlSnapMissingAssertions(c *C) {
	s.makeSnapInSeed(c, snapdYaml)
	s.makeSnapInSeed(c, packageCore18)
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0
base: core18
`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: snapd
   file: snapd_1.snap
 - name: some-snap
   file: some-snap_1.snap
 - name: core18
   file: core18_1.snap
`)

	err := os.Remove(filepath.Join(s.AssertsDir(), "some-snap.asserts"))
	c.Assert(err, IsNil)

	err = seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot find signatures with metadata for snap "some-snap" .*`)

}

func (s *validateSuite) TestValidateFromYamlDuplicatedSnap(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	s.makeSnapInSeed(c, `name: gtk-common-themes
version: 19.04`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   channel: stable
   file: core_1.snap
 - name: gtk-common-themes
   channel: stable/ubuntu-19.04
   file: gtk-common-themes_1.snap
 - name: gtk-common-themes
   channel: stable/ubuntu-19.04
   file: gtk-common-themes_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot read seed yaml: snap name "gtk-common-themes" must be unique`)
}

func (s *validateSuite) TestValidateFromYamlRegressionLP1825437(c *C) {
	s.makeSnapInSeed(c, coreYaml)
	s.makeSnapInSeed(c, `name: gtk-common-themes
version: 19.04`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 -
   name: core
   channel: stable
   file: core_1.snap
 -
 -
   name: gtk-common-themes
   channel: stable/ubuntu-19.04
   file: gtk-common-themes_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - cannot read seed yaml: empty element in seed`)
}

func (s *validateSuite) TestValidateErrorSingle(c *C) {
	err := seed.ValidationError{
		SystemErrors: map[string][]error{
			"system-1": {fmt.Errorf("err1")},
		},
	}
	c.Check(err.Error(), Equals, `cannot validate seed system "system-1":
 - err1`)
}

func (s *validateSuite) TestValidateErrorMulti(c *C) {
	err1 := errors.New("err1")
	err2 := errors.New("err2")
	err := seed.ValidationError{
		SystemErrors: map[string][]error{
			"system-1": {err1},
			"system-2": {err1, err2},
		},
	}
	c.Check(err.Error(), Equals, `cannot validate seed system "system-1":
 - err1
and seed system "system-2":
 - err1
 - err2`)
}
