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

package common

import (
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// testing a testsuite - thats so meta
type MetaTestSuite struct {
}

var _ = Suite(&MetaTestSuite{})

func (m *MetaTestSuite) TestCleanup(c *C) {
	canary := "not-called"
	s := SnappySuite{}

	s.AddCleanup(func() {
		canary = "was-called"
	})
	s.TearDownTest(c)

	c.Assert(canary, Equals, "was-called")
}
