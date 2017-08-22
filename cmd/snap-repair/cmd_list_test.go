// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"os"

	. "gopkg.in/check.v1"

	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
)

func (r *repairSuite) TestListNoRepairsYet(c *C) {
	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, "no repairs yet\n")
}

func (r *repairSuite) TestListRepairsSimple(c *C) {
	err := os.MkdirAll(dirs.SnapRepairDir, 0775)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapRepairStateFile, []byte(`
{
  "device": {
    "brand": "my-brand",
    "model": "my-model"
  },
  "sequences": {
    "canonical": [
      {"sequence":1,"revision":3,"status":0}
    ],
    "my-brand": [
      {"sequence":1,"revision":1,"status":2},
      {"sequence":2,"revision":2,"status":1}
    ]
  }
}
`), 0600)
	c.Assert(err, IsNil)

	err = repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `Issuer     Seq  Rev  Status
canonical  1    3    retry
my-brand   1    1    done
my-brand   2    2    skip
`)

}
