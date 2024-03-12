// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type outputErrSuite struct{}

var _ = Suite(&outputErrSuite{})

func (ts *outputErrSuite) TestOutputErrOutputWithoutNewlines(c *C) {
	output := "test output"
	err := fmt.Errorf("test error")
	formattedErr := osutil.OutputErr([]byte(output), err)
	c.Check(formattedErr, ErrorMatches, output)
	formattedErr = osutil.OutputErrCombine([]byte(output), nil, err)
	c.Check(formattedErr, ErrorMatches, output)
	formattedErr = osutil.OutputErrCombine([]byte(output), []byte{}, err)
	c.Check(formattedErr, ErrorMatches, output)
}

func (ts *outputErrSuite) TestOutputErrOutputWithNewlines(c *C) {
	output := "output line1\noutput line2"
	err := fmt.Errorf("test error")
	formattedErr := osutil.OutputErr([]byte(output), err)
	c.Check(formattedErr.Error(), Equals, `
-----
output line1
output line2
-----`)
}

func (ts *outputErrSuite) TestOutputErrNoOutput(c *C) {
	err := fmt.Errorf("test error")
	formattedErr := osutil.OutputErr([]byte{}, err)
	c.Check(formattedErr, Equals, err)
}

func (ts *outputErrSuite) TestOutputErrOutputWithStderr(c *C) {
	output := "test output"
	stderr := "something bad happened"
	err := fmt.Errorf("test error")
	formattedErr := osutil.OutputErrCombine([]byte(output), []byte(stderr), err)
	c.Check(formattedErr.Error(), Equals, "\n-----\n"+output+"\nstderr:\n"+stderr+
		"\n-----")
}
