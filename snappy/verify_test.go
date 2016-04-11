// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	. "gopkg.in/check.v1"
)

func (s *SnapTestSuite) TestServiceWhitelistSimple(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Description: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Stop: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{PostStop: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestServiceWhitelistIllegal(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "x\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Description: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Stop: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{PostStop: "foo\n"}), NotNil)
}

func (s *SnapTestSuite) TestVerifyAppDaemonValue(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Daemon: "oneshot"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Daemon: "nono"}), ErrorMatches, `"daemon" field contains invalid value "nono"`)
}

func (s *SnapTestSuite) TestServiceWhitelistError(c *C) {
	err := verifyAppYaml(&AppYaml{Name: "x\n"})
	c.Assert(err.Error(), Equals, `app description field 'Name' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:-]*$')`)
}

func (s *SnapTestSuite) TestBinariesWhitelistSimple(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestBinariesWhitelistIllegal(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "test!me"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Name: "x\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "x\n"}), NotNil)
}
