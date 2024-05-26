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

package apparmor_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type appArmorSuite struct {
	testutil.BaseTest
}

var _ = Suite(&appArmorSuite{})

func (s *appArmorSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *appArmorSuite) TestValidateNoAppArmorRegexp(c *C) {
	for _, testData := range []struct {
		inputString   string
		expectedError string
	}{
		{"", ""},
		{"This is f1ne!", ""},
		{"No questions?", `"No questions\?" contains a reserved apparmor char.*`},
		{"Brackets[]", `"Brackets\[\]" contains a reserved apparmor char.*`},
		{"Braces{}", `"Braces{}" contains a reserved apparmor char.*`},
		{"Star*", `"Star\*" contains a reserved apparmor char.*`},
		{"hat^", `"hat\^" contains a reserved apparmor char.*`},
		{`double"quotes`, `"double\\"quotes" contains a reserved apparmor char.*`},
	} {
		testLabel := Commentf("input: %s", testData.inputString)
		mylog.Check(apparmor.ValidateNoAppArmorRegexp(testData.inputString))
		if testData.expectedError != "" {
			c.Check(err, ErrorMatches, testData.expectedError, testLabel)
		} else {
			c.Check(err, IsNil, testLabel)
		}
	}
}
