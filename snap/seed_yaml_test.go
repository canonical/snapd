// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

type seedYamlTestSuite struct{}

var _ = Suite(&seedYamlTestSuite{})

var mockSeedYaml = []byte(`
snaps:
 - name: foo
   snap-id: snapidsnapidsnapid
   channel: stable
   devmode: true
   file: foo_1.0_all.snap
 - name: local
   unasserted: true
   file: local.snap
`)

func (s *seedYamlTestSuite) TestSimple(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, mockSeedYaml, 0644)
	c.Assert(err, IsNil)

	seed, err := snap.ReadSeedYaml(fn)
	c.Assert(err, IsNil)
	c.Assert(seed.Snaps, HasLen, 2)
	c.Assert(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		File:   "foo_1.0_all.snap",
		Name:   "foo",
		SnapID: "snapidsnapidsnapid",

		Channel: "stable",
		DevMode: true,
	})
	c.Assert(seed.Snaps[1], DeepEquals, &snap.SeedSnap{
		File:       "local.snap",
		Name:       "local",
		Unasserted: true,
	})
}

var badMockSeedYaml = []byte(`
snaps:
 - name: foo
   file: foo/bar.snap
`)

func (s *seedYamlTestSuite) TestNoPathAllowed(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, badMockSeedYaml, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: "foo/bar.snap" must be a filename, not a path`)
}

func (s *seedYamlTestSuite) TestDuplicatedSnapName(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - name: foo
   channel: stable
   file: foo_1.0_all.snap
 - name: foo
   channel: edge
   file: bar_1.0_all.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: snap name "foo" must be unique`)
}

func (s *seedYamlTestSuite) TestValidateChannelUnhappy(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - name: foo
   channel: invalid/channel/
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: invalid risk in channel name: invalid/channel/`)
}

func (s *seedYamlTestSuite) TestValidateNameUnhappy(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - name: invalid--name
   file: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: invalid snap name: "invalid--name"`)
}

func (s *seedYamlTestSuite) TestValidateNameInstanceUnsupported(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - name: foo_1
   file: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: invalid snap name: "foo_1"`)
}

func (s *seedYamlTestSuite) TestValidateNameMissing(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - file: ./foo.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: invalid snap name: ""`)
}

func (s *seedYamlTestSuite) TestValidateFileMissing(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, []byte(`
snaps:
 - name: foo
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadSeedYaml(fn)
	c.Assert(err, ErrorMatches, `cannot read seed yaml: "file" attribute for "foo" cannot be empty`)
}
