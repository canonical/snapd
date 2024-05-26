// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	. "github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

type interfaceNilCheckerSuite struct{}

var _ = Suite(&interfaceNilCheckerSuite{})

type someError struct{}

func (*someError) Error() string { return "" }

func (*interfaceNilCheckerSuite) TestIsInterfaceNilMatchesIntendedNilComparison(c *C) {
	testInfo(c, IsInterfaceNil, "IsInterfaceNil", []string{"value"})

	err1 := func() error {
		return nil
	}()
	c.Assert(err1 == nil, Equals, true)
	c.Assert(err1, IsInterfaceNil)

	err2 := func() error {
		return err
	}()
	c.Assert(err2 == nil, Equals, true)
	c.Assert(err2, IsInterfaceNil)

	// assigning nil to a typed variable and then returning it fails the '== nil' check
	err3 := typedErrWithNilValue()
	c.Assert(err3 == nil, Equals, false)
	res, errMsg := IsInterfaceNil.Check([]interface{}{err3}, []string{"value"})
	c.Assert(res, Equals, false)
	c.Assert(errMsg, Equals, "expected <nil> but got *testutil_test.someError type")
}

// returns an error with a *someError type but nil value
func typedErrWithNilValue() error {
	err := &someError{}
	// prevent inefassign from complaining
	_ = err
	err = nil
	return err
}
