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

package main_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDebugValidateSeedRegressionLp1825437(c *C) {
	tmpf := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(tmpf, []byte(`
snaps:
 -
   name: core
   channel: stable
   file: core_6673.snap
 -
 -
   name: gnome-foo
   channel: stable/ubuntu-19.04
   file: gtk-common-themes_1198.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"debug", "validate-seed", tmpf})
	c.Assert(err, ErrorMatches, "cannot read seed yaml: empty element in seed")
}

func (s *SnapSuite) TestDebugValidateSeedDuplicatedSnap(c *C) {
	tmpf := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(tmpf, []byte(`
snaps:
 - name: foo
   file: foo.snap
 - name: foo
   file: bar.snap
`), 0644)
	c.Assert(err, IsNil)

	_, err = snap.Parser(snap.Client()).ParseArgs([]string{"debug", "validate-seed", tmpf})
	c.Assert(err, ErrorMatches, `cannot read seed yaml: snap name "foo" must be unique`)
}
