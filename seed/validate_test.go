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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/testutil"
)

type validateSuite struct {
	testutil.BaseTest
	root string
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
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.root = c.MkDir()

	err := os.MkdirAll(filepath.Join(s.root, "snaps"), 0755)
	c.Assert(err, IsNil)
}

func (s *validateSuite) makeSnapInSeed(c *C, snapYaml string) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)

	src := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	dst := filepath.Join(s.root, "snaps", fmt.Sprintf("%s_%s.snap", info.SnapName(), snap.R(1)))
	c.Assert(os.Rename(src, dst), IsNil)
}

func (s *validateSuite) makeSeedYaml(c *C, seedYaml string) string {
	tmpf := filepath.Join(s.root, "seed.yaml")
	err := ioutil.WriteFile(tmpf, []byte(seedYaml), 0644)
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
- cannot use snap "need-df": default provider "gtk-common-themes" is missing`)
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

func (s *validateSuite) TestValidateFromYamlSnapMissingSnapdAndCore(c *C) {
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
- the core or snapd snap must be part of the seed`)
}

func (s *validateSuite) TestValidateFromYamlSnapMultipleErrors(c *C) {
	s.makeSnapInSeed(c, `name: some-snap
version: 1.0`)
	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: some-snap
   file: some-snap_1.snap
`)

	err := seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
- the core or snapd snap must be part of the seed
- cannot use snap "some-snap": required snap "core" missing`)
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
- cannot open snap: open /.*/snaps/some-snap_1.snap: no such file or directory`)
}

func (s *validateSuite) TestValidateFromYamlSnapSnapInvalid(c *C) {
	s.makeSnapInSeed(c, coreYaml)

	// "version" is missing in this yaml
	snapBuildDir := c.MkDir()
	snapYaml := `name: some-snap-invalid-yaml`
	metaSnapYaml := filepath.Join(snapBuildDir, "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(metaSnapYaml), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(metaSnapYaml, []byte(snapYaml), 0644)
	c.Assert(err, IsNil)

	// need to build the snap "manually" pack.Snap() will do validation
	snapFilePath := filepath.Join(c.MkDir(), "some-snap-invalid-yaml_1.snap")
	d := squashfs.New(snapFilePath)
	err = d.Build(snapBuildDir, nil)
	c.Assert(err, IsNil)

	// put the broken snap in place
	dst := filepath.Join(s.root, "snaps", "some-snap-invalid-yaml_1.snap")
	err = os.Rename(snapFilePath, dst)
	c.Assert(err, IsNil)

	seedFn := s.makeSeedYaml(c, `
snaps:
 - name: core
   file: core_1.snap
 - name: some-snap-invalid-yaml
   file: some-snap-invalid-yaml_1.snap
`)

	err = seed.ValidateFromYaml(seedFn)
	c.Assert(err, ErrorMatches, `cannot validate seed:
- cannot use snap /.*/snaps/some-snap-invalid-yaml_1.snap: invalid snap version: cannot be empty`)
}
