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
	"path/filepath"

	. "gopkg.in/check.v1"

	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
)

func (r *repairSuite) TestListNoRepairsYet(c *C) {
	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, "no repairs yet\n")
}

func makeMockRepairState(c *C) {
	// FIXME: we don't use the state
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

	// the canonical script dir content
	basedir := filepath.Join(dirs.SnapRepairRunDir, "canonical/1")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.000001.2017-08-22T101401.output"), []byte("foo"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.000001.2017-08-22T101401.retry"), nil, 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "script.r3"), []byte("#!/bin/sh\necho foo"), 0700)
	c.Assert(err, IsNil)

	// my-brand
	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/1")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.000001.2017-08-21T101401.output"), []byte("bar"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.000001.2017-08-21T101401.done"), nil, 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "script.r1"), []byte("#!/bin/sh\necho bar"), 0700)
	c.Assert(err, IsNil)

	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/2")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.000001.2017-08-23T101401.output"), []byte("baz"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.000001.2017-08-23T101401.skip"), nil, 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "script.r1"), []byte("#!/bin/sh\necho baz"), 0700)
	c.Assert(err, IsNil)
}

func (r *repairSuite) TestListRepairsSimple(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `Issuer     Seq  Rev  Status
canonical  1    3    retry
my-brand   1    1    done
my-brand   2    2    skip
`)
}

func (r *repairSuite) TestListRepairsVerbose(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"list", "--verbose"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `Issuer     Seq  Rev  Status
canonical  1    3    retry
 output:
  foo
 script:
  #!/bin/sh
  echo foo
my-brand  1    1    done
 output:
  bar
 script:
  #!/bin/sh
  echo bar
my-brand  2    2    skip
 output:
  baz
 script:
  #!/bin/sh
  echo baz
`)

}
