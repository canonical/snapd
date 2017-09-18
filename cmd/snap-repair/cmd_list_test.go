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
	// the canonical script dir content
	basedir := filepath.Join(dirs.SnapRepairRunDir, "canonical/1")
	err := os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.retry"), []byte("repair: canonical-1\nsummary: repair one\noutput:\nretry output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r3.script"), []byte("#!/bin/sh\necho retry output"), 0700)
	c.Assert(err, IsNil)

	// my-brand
	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/1")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.done"), []byte("repair: my-brand-1\nsummary: my-brand repair one\noutput:\ndone output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r1.script"), []byte("#!/bin/sh\necho done output"), 0700)
	c.Assert(err, IsNil)

	basedir = filepath.Join(dirs.SnapRepairRunDir, "my-brand/2")
	err = os.MkdirAll(basedir, 0700)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.skip"), []byte("repair: my-brand-2\nsummary: my-brand repair two\noutput:\nskip output"), 0600)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(basedir, "r2.script"), []byte("#!/bin/sh\necho skip output"), 0700)
	c.Assert(err, IsNil)
}

func (r *repairSuite) TestListRepairsSimple(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `Repair       Rev  Status  Summary
canonical-1  3    retry   repair one
my-brand-1   1    done    my-brand repair one
my-brand-2   2    skip    my-brand repair two
`)
}
