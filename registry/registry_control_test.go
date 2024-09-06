// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package registry_test

import (
	"github.com/snapcore/snapd/registry"
	. "gopkg.in/check.v1"
)

type rgctrlSuite struct{}

var _ = Suite(&rgctrlSuite{})

func (s *rgctrlSuite) TestNewRegistryControl(c *C) {
	type testcase struct {
		operatorID string
		views      []interface{}
		err        string
	}

	tcs := []testcase{
		{
			operatorID: "@op-id2",
			err:        "invalid Operator ID @op-id2",
		},
		{
			operatorID: "canonical",
			err:        "cannot define registry-control: no views provided",
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{}},
			err:        "view at position 1: must be a non-empty map",
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"other-field": []interface{}{}}},
			err:        `view at position 1: "name" not provided`,
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"name": []interface{}{}}},
			err:        `view at position 1: "name" must be a non-empty string`,
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"name": "a/b/c/d"}},
			err:        `view at position 1: "name" must be in the format account/registry/view: a/b/c/d`,
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"name": "@foo/network/control-device"}},
			err:        "view at position 1: invalid Account ID @foo",
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"name": "canonical/123/control-device"}},
			err:        "view at position 1: invalid registry name 123",
		},
		{
			operatorID: "canonical",
			views:      []interface{}{map[string]interface{}{"name": "canonical/network/_view"}},
			err:        "view at position 1: invalid view name _view",
		},
		{
			operatorID: "canonical",
			views: []interface{}{
				map[string]interface{}{"name": "canonical/network/control-interface"},
				map[string]interface{}{"name": "canonical/network/observe-interface"},
			},
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		rgCtrl, err := registry.NewRegistryControl(tc.operatorID, tc.views)
		if tc.err != "" {
			c.Assert(err, NotNil)
			c.Assert(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(rgCtrl, NotNil, cmt)
		}
	}
}

func (s *rgctrlSuite) TestDelegate(c *C) {
	rgCtrl, err := registry.NewRegistryControl(
		"canonical",
		[]interface{}{
			map[string]interface{}{"name": "canonical/network/control-interface"},
			map[string]interface{}{"name": "canonical/network/observe-interface"},
		},
	)
	c.Assert(err, IsNil)

	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, false)

	err = rgCtrl.Delegate("canonical", "network", "control-vpn")
	c.Assert(err, IsNil)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, true)

	// test idempotency
	err = rgCtrl.Delegate("canonical", "network", "control-vpn")
	c.Assert(err, IsNil)
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-vpn"), Equals, true)
}

func (s *rgctrlSuite) TestRevoke(c *C) {
	rgCtrl, err := registry.NewRegistryControl(
		"canonical",
		[]interface{}{
			map[string]interface{}{"name": "canonical/network/control-interface"},
			map[string]interface{}{"name": "canonical/network/observe-interface"},
		},
	)
	c.Assert(err, IsNil)

	c.Assert(len(rgCtrl.Registries), Equals, 1) // canonical/network
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-interface"), Equals, true)

	rgCtrl.Revoke("canonical", "network", "control-interface")
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-interface"), Equals, false)

	// test idempotency
	rgCtrl.Revoke("canonical", "network", "control-interface")
	c.Check(rgCtrl.IsDelegated("canonical", "network", "control-interface"), Equals, false)

	// empty delegation
	rgCtrl.Revoke("canonical", "network", "observe-interface")
	c.Assert(len(rgCtrl.Registries), Equals, 0)
}
