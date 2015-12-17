// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package common

import (
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

// testing a testsuite - thats so meta
type MetaTestSuite struct {
}

var _ = check.Suite(&MetaTestSuite{})

// test trivial cleanup
func (m *MetaTestSuite) TestCleanupSimple(c *check.C) {
	canary := "not-called"
	s := SnappySuite{}

	s.AddCleanup(func() {
		canary = "was-called"
	})
	s.TearDownTest(c)

	c.Assert(canary, check.Equals, "was-called")
}

// a mock method that takes a parameter
func mockCleanupMethodWithParameters(s *string) {
	*s = "was-called"
}

// test that whle AddCleanup() does not take any parameters itself,
// functions that need parameters can be passed by creating an
// anonymous function as a wrapper
func (m *MetaTestSuite) TestCleanupWithParameters(c *check.C) {
	canary := "not-called"
	s := SnappySuite{}

	s.AddCleanup(func() {
		mockCleanupMethodWithParameters(&canary)
	})
	s.TearDownTest(c)

	c.Assert(canary, check.Equals, "was-called")
}
