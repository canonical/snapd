// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package polkit_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *polkit.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		PolkitConnectedPlugCallback: func(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			policyErr := spec.AddPolicy("connected-plug", polkit.Policy("policy-connected-plug"))
			ruleErr := spec.AddRule("connected-plug", polkit.Rule("rule-connected-plug"))
			return strutil.JoinErrors(policyErr, ruleErr)
		},
		PolkitConnectedSlotCallback: func(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			policyErr := spec.AddPolicy("connected-slot", polkit.Policy("policy-connected-slot"))
			ruleErr := spec.AddRule("connected-slot", polkit.Rule("rule-connected-slot"))
			return strutil.JoinErrors(policyErr, ruleErr)
		},
		PolkitPermanentPlugCallback: func(spec *polkit.Specification, plug *snap.PlugInfo) error {
			policyErr := spec.AddPolicy("permanent-plug", polkit.Policy("policy-permanent-plug"))
			ruleErr := spec.AddRule("permanent-plug", polkit.Rule("rule-permanent-plug"))
			return strutil.JoinErrors(policyErr, ruleErr)
		},
		PolkitPermanentSlotCallback: func(spec *polkit.Specification, slot *snap.SlotInfo) error {
			policyErr := spec.AddPolicy("permanent-slot", polkit.Policy("policy-permanent-slot"))
			ruleErr := spec.AddRule("permanent-slot", polkit.Rule("rule-permanent-slot"))
			return strutil.JoinErrors(policyErr, ruleErr)
		},
	},
})

func (s *specSuite) SetUpTest(c *C) {
	s.spec = &polkit.Specification{}
	const plugYaml = `name: snap1
version: 1
apps:
 app1:
  plugs: [name]
`
	s.plug, s.plugInfo = ifacetest.MockConnectedPlug(c, plugYaml, nil, "name")

	const slotYaml = `name: snap2
version: 1
slots:
 name:
  interface: test
apps:
 app2:
`
	s.slot, s.slotInfo = ifacetest.MockConnectedSlot(c, slotYaml, nil, "name")
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Policies(), DeepEquals, map[string]polkit.Policy{
		"connected-plug": polkit.Policy("policy-connected-plug"),
		"connected-slot": polkit.Policy("policy-connected-slot"),
		"permanent-plug": polkit.Policy("policy-permanent-plug"),
		"permanent-slot": polkit.Policy("policy-permanent-slot"),
	})
	c.Assert(s.spec.Rules(), DeepEquals, map[string]polkit.Rule{
		"connected-plug": polkit.Rule("rule-connected-plug"),
		"connected-slot": polkit.Rule("rule-connected-slot"),
		"permanent-plug": polkit.Rule("rule-permanent-plug"),
		"permanent-slot": polkit.Rule("rule-permanent-slot"),
	})
}

func (s *specSuite) TestSpecificationIfaceAddPolicyOverwriteError(c *C) {
	c.Assert(s.spec.AddPolicy("test", polkit.Policy("content 1")), IsNil)
	c.Assert(s.spec.AddPolicy("test", polkit.Policy("content 2")), ErrorMatches, "internal error: polkit policy content for \"test\" re-defined with different content")
}

func (s *specSuite) TestSpecificationIfaceAddRuleOverwriteError(c *C) {
	c.Assert(s.spec.AddRule("test", polkit.Rule("content 1")), IsNil)
	c.Assert(s.spec.AddRule("test", polkit.Rule("content 2")), ErrorMatches, "internal error: polkit rule content for \"test\" re-defined with different content")
}

func (s *specSuite) TestSpecificationIfaceAddRuleInvalidSuffix(c *C) {
	c.Assert(s.spec.AddRule("?", polkit.Rule("content")), ErrorMatches, `"\?" does not match .*`)
	c.Assert(s.spec.AddRule("..", polkit.Rule("content")), ErrorMatches, `"\.\." does not match .*`)
}
