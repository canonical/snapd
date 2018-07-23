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
	. "gopkg.in/check.v1"

	repair "github.com/snapcore/snapd/cmd/snap-repair"
)

func (r *repairSuite) TestListNoRepairsYet(c *C) {
	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, "")
	c.Check(r.Stderr(), Equals, "no repairs yet\n")
}

func (r *repairSuite) TestListRepairsSimple(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"list"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `Repair       Rev  Status   Summary
canonical-1  3    retry    repair one
my-brand-1   1    done     my-brand repair one
my-brand-2   2    skip     my-brand repair two
my-brand-3   0    running  my-brand repair three
`)
	c.Check(r.Stderr(), Equals, "")
}
