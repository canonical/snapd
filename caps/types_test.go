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
	"encoding/json"
	"fmt"

	. "gopkg.in/check.v1"
)

type TypeSuite struct{}

var _ = Suite(&TypeSuite{})

// testType is only meant for testing. It is not useful in any way except
// that it offers an simple capability type that will happily validate.
var testType = &Type{
	Name:          "test",
	RequiredAttrs: nil,
}

func (s *TypeSuite) TestTypeString(c *C) {
	c.Assert(testType.String(), Equals, "test")
}

func (s *TypeSuite) TestValidateMismatchedType(c *C) {
	testType2 := &Type{Name: "test-two"} // Another test-like type that's not test itself
	cap := &Capability{Name: "name", Label: "label", Type: testType2}
	err := testType.Validate(cap)
	c.Assert(err, ErrorMatches, `capability is not of type "test"`)
}

func (s *TypeSuite) TestValidateOK(c *C) {
	cap := &Capability{Name: "name", Label: "label", Type: testType}
	err := testType.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *TypeSuite) TestValidateAttributesRequiredAttrsMissing(c *C) {
	t := &Type{
		Name:          "t",
		RequiredAttrs: []string{"k"},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	err := t.Validate(cap)
	c.Assert(err, ErrorMatches, `capabilities of type "t" must provide a "k" attribute`)
}

func (s *TypeSuite) TestValidateAttributesRequiredAttrsSatisfied(c *C) {
	t := &Type{
		Name:          "t",
		RequiredAttrs: []string{"k"},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
		Attrs: map[string]string{"k": "v"},
	}
	err := t.Validate(cap)
	c.Assert(err, IsNil)
}

func (s *TypeSuite) TestMarhshalJSON(c *C) {
	b, err := json.Marshal(testType)
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, []byte(`"test"`))
}

func (s *TypeSuite) TestUnmarhshalJSON(c *C) {
	var t Type
	err := json.Unmarshal([]byte(`"test"`), &t)
	c.Assert(err, IsNil)
	c.Assert(t.Name, Equals, "test")
}

func (s *TypeSuite) TestGrantPermissionsSuccess(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	t.GrantPermissions(snapName, cap)
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityGranted)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityGranted)
}

func (s *TypeSuite) TestGrantPermissionsFailure(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	// Configure mock security so that sec2 will fail the grant operation.
	sec2.SetGrantPermissionsError(snapName, fmt.Errorf("boom"))
	err := t.GrantPermissions(snapName, cap)
	c.Assert(err, ErrorMatches, "boom")
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityRevoked)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityInitial)
}

func (s *TypeSuite) TestGrantPermissionsCatastrophicFailure(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	// Configure mock security so that sec2 will fail the grant operation
	// and sec1 will fail the subsequent rollback (revoke) operation.
	sec2.SetGrantPermissionsError(snapName, fmt.Errorf("boom-granting"))
	sec1.SetRevokePermissionsError(snapName, fmt.Errorf("boom-revoking"))
	c.Assert(func() { t.GrantPermissions(snapName, cap) }, PanicMatches,
		`unable to revoke partially granted permissions: "boom-revoking"`)
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityGranted)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityInitial)
}

func (s *TypeSuite) TestRevokePermissionsSuccess(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	t.RevokePermissions(snapName, cap)
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityRevoked)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityRevoked)
}

func (s *TypeSuite) TestRevokePermissionsFailure(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	// Configure mock security so that sec2 will fail the revoke operation.
	sec2.SetRevokePermissionsError(snapName, fmt.Errorf("boom"))
	t.RevokePermissions(snapName, cap)
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityGranted)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityInitial)
}

func (s *TypeSuite) TestRevokePermissionsCatastropicFailure(c *C) {
	sec1 := &mockSecurity{}
	sec2 := &mockSecurity{}
	t := &Type{
		Name:            "t",
		SecuritySystems: []securitySystem{sec1, sec2},
	}
	cap := &Capability{
		Name:  "name",
		Label: "label",
		Type:  t,
	}
	snapName := "snap"
	// Configure mock security so that sec2 will fail the revoke operation
	// and sec1 will fail the subsequent rollback (grant) operation.
	sec2.SetRevokePermissionsError(snapName, fmt.Errorf("boom-revoking"))
	sec1.SetGrantPermissionsError(snapName, fmt.Errorf("boom-granting"))
	c.Assert(func() { t.RevokePermissions(snapName, cap) }, PanicMatches,
		`unable to grant partially revoked permissions: "boom-granting"`)
	c.Assert(sec1.StateMap[snapName], Equals, mockSecurityRevoked)
	c.Assert(sec2.StateMap[snapName], Equals, mockSecurityInitial)
}
