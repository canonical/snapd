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
   revision: 31
   file: foo_1.0_all.snap
`)

func (s *seedYamlTestSuite) TestSimple(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, mockSeedYaml, 0644)
	c.Assert(err, IsNil)

	seed, err := snap.ReadSeedYaml(fn)
	c.Assert(err, IsNil)
	c.Assert(seed.Snaps, HasLen, 1)
	c.Assert(seed.Snaps[0], DeepEquals, &snap.SeedSnap{
		File:     "foo_1.0_all.snap",
		RealName: "foo",
		SnapID:   "snapidsnapidsnapid",
		Channel:  "stable",
		Revision: snap.R(31),
	})
}
