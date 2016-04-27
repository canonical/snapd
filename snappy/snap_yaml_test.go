// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
)

type snapYamlTestSuite struct {
}

var _ = Suite(&snapYamlTestSuite{})

func (s *snapYamlTestSuite) TestParseYamlSetsTypeInUsesFromName(c *C) {
	snapYaml := []byte(`name: foo
version: 1.0
plugs:
 old-security:
  caps: []
`)
	sy, err := parseSnapYamlData(snapYaml, false)
	c.Assert(err, IsNil)
	sy.Plugs["old-security"].Interface = "old-security"

	_, err = snap.InfoFromSnapYaml(snapYaml)
	c.Assert(err, IsNil)
}

func (s *snapYamlTestSuite) TestParseEmptyPlugDef(c *C) {
	snapYaml := []byte(`name: foo
version: 1.0
plugs:
 unity7:
`)
	sy, err := parseSnapYamlData(snapYaml, false)
	c.Assert(err, IsNil)
	sy.Plugs["unity7"].Interface = "unity7"

	info, err := snap.InfoFromSnapYaml(snapYaml)
	c.Assert(err, IsNil)
	c.Check(info.Plugs["unity7"].Interface, Equals, "unity7")
}
