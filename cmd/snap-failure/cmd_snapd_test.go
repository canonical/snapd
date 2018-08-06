// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"os"

	. "gopkg.in/check.v1"

	failure "github.com/snapcore/snapd/cmd/snap-failure"
)

func (r *failureSuite) TestRun(c *C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"snap-failure", "snapd"}
	err := failure.Run()
	c.Check(err, IsNil)
	c.Check(r.Stdout(), HasLen, 0)
	c.Check(r.Stderr(), HasLen, 0)
}
