// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package policy_test

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	. "gopkg.in/check.v1"
)

type helpersSuite struct{}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) TestNestedGet(c *C) {
	consumer := snaptest.MockInfo(c, `
name: consumer
version: 0
apps:
    app:
plugs:
    plug:
        interface: interface
`, nil)
	plugInfo := consumer.Plugs["plug"]
	plug := interfaces.NewConnectedPlug(plugInfo, map[string]interface{}{
		"a": "123",
	})

	producer := snaptest.MockInfo(c, `
name: producer
version: 0
apps:
    app:
slots:
    slot:
        interface: interface
`, nil)
	slotInfo := producer.Slots["slot"]
	slot := interfaces.NewConnectedSlot(slotInfo, map[string]interface{}{
		"a": "123",
	})

	_, err := policy.NestedGet("slot", slot, "b")
	c.Check(err, ErrorMatches, `slot attribute "b" not found`)

	_, err = policy.NestedGet("plug", plug, "a.b")
	c.Check(err, ErrorMatches, `plug attribute "a\.b" not found`)

	v, err := policy.NestedGet("slot", slot, "a")
	c.Check(err, IsNil)
	c.Check(v, Equals, "123")

	slot = interfaces.NewConnectedSlot(slotInfo, map[string]interface{}{
		"a": map[string]interface{}{
			"b": []interface{}{"1", "2", "3"},
		},
	})

	v, err = policy.NestedGet("slot", slot, "a.b")
	c.Check(err, IsNil)
	c.Check(v, DeepEquals, []interface{}{"1", "2", "3"})
}

func (s *helpersSuite) TestSnapdTypeCheck(c *C) {
	// Type checking the snapd snap is done in a special way.
	// It appears to be of type "core" while in reality it is of type "app".
	sideInfo := &snap.SideInfo{
		SnapID: "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4",
	}
	snapInfo := snaptest.MockInfo(c, `
name: snapd
version: 1
type: app
slots:
    network:
`, sideInfo)
	err := policy.CheckSnapType(snapInfo, []string{"core"})
	c.Assert(err, IsNil)
}
