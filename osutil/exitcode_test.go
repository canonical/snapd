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

package osutil_test

import (
	"os"
	"os/exec"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type ExitCodeTestSuite struct{}

var _ = Suite(&ExitCodeTestSuite{})

func (ts *ExitCodeTestSuite) TestExitCode(c *C) {
	cmd := exec.Command("true")
	mylog.Check(cmd.Run())


	cmd = exec.Command("false")
	mylog.Check(cmd.Run())
	c.Assert(err, NotNil)
	e := mylog.Check2(osutil.ExitCode(err))

	c.Assert(e, Equals, 1)

	cmd = exec.Command("sh", "-c", "exit 7")
	mylog.Check(cmd.Run())
	e = mylog.Check2(osutil.ExitCode(err))

	c.Assert(e, Equals, 7)

	// ensure that non exec.ExitError values give a error
	_ = mylog.Check2(os.Stat("/random/file/that/is/not/there"))
	c.Assert(err, NotNil)
	_ = mylog.Check2(osutil.ExitCode(err))
	c.Assert(err, NotNil)
}
