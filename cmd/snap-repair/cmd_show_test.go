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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	repair "github.com/snapcore/snapd/cmd/snap-repair"
	"github.com/snapcore/snapd/dirs"
)

func (r *repairSuite) TestShowRepairSingle(c *C) {
	makeMockRepairState(c)

	err := repair.ParseArgs([]string{"show", "canonical-1"})
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `repair: canonical-1
revision: 3
status: retry
summary: repair one
script:
  #!/bin/sh
  echo retry output
output:
  retry output

`)

}

func (r *repairSuite) TestShowRepairMultiple(c *C) {
	makeMockRepairState(c)

	// repair.ParseArgs() always appends to its internal slice:
	// cmdShow.Positional.Repair. To workaround this we create a
	// new cmdShow here
	err := repair.NewCmdShow("canonical-1", "my-brand-1", "my-brand-2").Execute(nil)
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, `repair: canonical-1
revision: 3
status: retry
summary: repair one
script:
  #!/bin/sh
  echo retry output
output:
  retry output

repair: my-brand-1
revision: 1
status: done
summary: my-brand repair one
script:
  #!/bin/sh
  echo done output
output:
  done output

repair: my-brand-2
revision: 2
status: skip
summary: my-brand repair two
script:
  #!/bin/sh
  echo skip output
output:
  skip output

`)
}

func (r *repairSuite) TestShowRepairErrorNoRepairDir(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := repair.NewCmdShow("canonical-1").Execute(nil)
	c.Check(err, ErrorMatches, `cannot find repair "canonical-1"`)
}

func (r *repairSuite) TestShowRepairSingleWithoutScript(c *C) {
	makeMockRepairState(c)
	scriptPath := filepath.Join(dirs.SnapRepairRunDir, "canonical/1", "r3.script")
	err := os.Remove(scriptPath)
	c.Assert(err, IsNil)

	err = repair.NewCmdShow("canonical-1").Execute(nil)
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, fmt.Sprintf(`repair: canonical-1
revision: 3
status: retry
summary: repair one
script:
  error: open %s: no such file or directory
output:
  retry output

`, scriptPath))

}

func (r *repairSuite) TestShowRepairSingleUnreadableOutput(c *C) {
	makeMockRepairState(c)
	scriptPath := filepath.Join(dirs.SnapRepairRunDir, "canonical/1", "r3.retry")
	err := os.Chmod(scriptPath, 0000)
	c.Assert(err, IsNil)
	defer os.Chmod(scriptPath, 0644)

	err = repair.NewCmdShow("canonical-1").Execute(nil)
	c.Check(err, IsNil)
	c.Check(r.Stdout(), Equals, fmt.Sprintf(`repair: canonical-1
revision: 3
status: retry
summary: -
script:
  #!/bin/sh
  echo retry output
output:
  error: open %s: permission denied

`, scriptPath))

}
