// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"fmt"

	. "gopkg.in/check.v1"
)

type MockSecuritySuite struct{}

var _ = Suite(&MockSecuritySuite{})

func (s *MockSecuritySuite) TestGrantPermissionSuccess(c *C) {
	const snapName = "snap"
	sec := &mockSecurity{}
	err := sec.GrantPermissions(snapName, testCapability)
	c.Assert(err, IsNil)
	c.Assert(sec.StateMap[snapName], Equals, mockSecurityGranted)
}

func (s *MockSecuritySuite) TestGrantPermissionFailure(c *C) {
	const snapName = "snap"
	sec := &mockSecurity{}
	sec.SetGrantPermissionsError(snapName, fmt.Errorf("boom"))
	err := sec.GrantPermissions(snapName, testCapability)
	c.Assert(err, ErrorMatches, "boom")
	c.Assert(sec.StateMap[snapName], Equals, mockSecurityInitial)
}

func (s *MockSecuritySuite) TestRevokePermissionSuccess(c *C) {
	const snapName = "snap"
	sec := &mockSecurity{}
	err := sec.RevokePermissions(snapName, testCapability)
	c.Assert(err, IsNil)
	c.Assert(sec.StateMap[snapName], Equals, mockSecurityRevoked)
}

func (s *MockSecuritySuite) TestRevokePermissionFailure(c *C) {
	const snapName = "snap"
	sec := &mockSecurity{}
	sec.SetRevokePermissionsError(snapName, fmt.Errorf("boom"))
	err := sec.RevokePermissions(snapName, testCapability)
	c.Assert(err, ErrorMatches, "boom")
	c.Assert(sec.StateMap[snapName], Equals, mockSecurityInitial)
}
