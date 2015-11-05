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

import "gopkg.in/check.v1"

type infoTestSuite struct {
	execReturnValue string
	backExecCommand func(*check.C, ...string) string
}

var _ = check.Suite(&infoTestSuite{})

func (s *infoTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
}

func (s *infoTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
}

func (s *infoTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	return s.execReturnValue
}

var releaseTests = []struct {
	infoOutput      string
	expectedRelease string
}{
	{"someInfo1: someValue1\nrelease: ubuntu-core/15.04/edge\nsomeInfo2: someValue2", "15.04"},
	{"someInfo1: someValue1\nrelease: ubuntu-core/rolling/alpha\nsomeInfo2: someValue2", "rolling"},
}

func (s *infoTestSuite) TestRelease(c *check.C) {
	for _, t := range releaseTests {
		s.execReturnValue = t.infoOutput
		release := Release(c)
		c.Assert(release, check.Equals, t.expectedRelease, check.Commentf("Wrong release"))
	}
}
