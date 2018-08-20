// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"bytes"
	"os/exec"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type unameSuite struct{}

var _ = check.Suite(unameSuite{})

func ucmd1(c *check.C, arg string) string {
	out, err := exec.Command("uname", arg).CombinedOutput()
	c.Assert(err, check.IsNil)
	return string(bytes.TrimSpace(out))
}

func (unameSuite) TestUname(c *check.C) {
	c.Check(osutil.KernelVersion(), check.Equals, ucmd1(c, "-r"))
	c.Check(osutil.MachineName(), check.Equals, ucmd1(c, "-m"))
}
