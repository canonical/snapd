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

func (r *repairSuite) TestShowRepairMultiple(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"show", "canonical-1", "my-brand-1", "my-brand-2"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `canonical-1  3  retry
 output:
  retry output
 script:
  #!/bin/sh
  echo retry output

my-brand-1  1  done
 output:
  done output
 script:
  #!/bin/sh
  echo done output

my-brand-2  2  skip
 output:
  skip output
 script:
  #!/bin/sh
  echo skip output

`)
}
