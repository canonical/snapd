// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package testutil_test

import (
	"errors"

	. "github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type errorIsCheckerSuite struct{}

var _ = Suite(&errorIsCheckerSuite{})

type baseError struct{}

func (baseError) Error() string { return "" }

func (baseError) Is(err error) bool {
	_, ok := err.(baseError)
	return ok
}

type wrapperError struct {
	err error
}

func (*wrapperError) Error() string { return "" }

func (e *wrapperError) Unwrap() error { return e.err }

func (*errorIsCheckerSuite) TestErrorIsCheckSucceeds(c *C) {
	testInfo(c, ErrorIs, "ErrorIs", []string{"error", "target"})

	c.Assert(baseError{}, ErrorIs, baseError{})
	err := &wrapperError{err: baseError{}}
	c.Assert(err, ErrorIs, baseError{})
}

func (*errorIsCheckerSuite) TestErrorIsCheckFails(c *C) {
	c.Assert(nil, Not(ErrorIs), baseError{})
	c.Assert(errors.New(""), Not(ErrorIs), baseError{})
}

func (*errorIsCheckerSuite) TestErrorIsWithInvalidArguments(c *C) {
	res, errMsg := ErrorIs.Check([]interface{}{"", errors.New("")}, []string{"error", "target"})
	c.Assert(res, Equals, false)
	c.Assert(errMsg, Equals, "first argument must be an error")

	res, errMsg = ErrorIs.Check([]interface{}{errors.New(""), ""}, []string{"error", "target"})
	c.Assert(res, Equals, false)
	c.Assert(errMsg, Equals, "second argument must be an error")

}
