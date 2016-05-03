// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package build

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"
)

type snapBuildTestSuite struct{}

var _ = check.Suite(&snapBuildTestSuite{})

const snapYamlContent = `name: basic
version: 1.0
summary: Basic snap
`

func (s *snapBuildTestSuite) TestLocalSnap(c *check.C) {
	tmpdir := c.MkDir()

	prev := baseSnapPath
	baseSnapPath = tmpdir
	defer func() {
		baseSnapPath = prev
	}()

	snapdir := filepath.Join(tmpdir, "basic")

	os.MkdirAll(filepath.Join(snapdir, "meta"), 0755)

	snapYaml := filepath.Join(snapdir, "meta", "snap.yaml")
	err := ioutil.WriteFile(snapYaml, []byte(snapYamlContent), 0644)
	c.Assert(err, check.IsNil)

	outSnap, err := LocalSnap(c, "basic")
	c.Assert(err, check.IsNil)

	c.Check(outSnap, check.Equals, filepath.Join(snapdir, "basic_1.0_all.snap"))
	stat, err := os.Stat(outSnap)
	c.Assert(err, check.IsNil)
	c.Check(stat.Size(), check.Not(check.Equals), 0)

}
