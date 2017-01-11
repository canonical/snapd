// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2017 Canonical Ltd
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

package ifacetest_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
)

type TestRecorderSuite struct {
	iface *ifacetest.TestInterface
	rec   *ifacetest.TestRecorder
	plug  *interfaces.Plug
	slot  *interfaces.Slot
}

var _ = Suite(&TestRecorderSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		RecordTestConnectedPlugCallback: func(rec *ifacetest.TestRecorder, plug *interfaces.Plug, slot *interfaces.Slot) error {
			rec.AddSnippet("connected-plug")
			return nil
		},
		RecordTestConnectedSlotCallback: func(rec *ifacetest.TestRecorder, plug *interfaces.Plug, slot *interfaces.Slot) error {
			rec.AddSnippet("connected-slot")
			return nil
		},
		RecordTestPermanentPlugCallback: func(rec *ifacetest.TestRecorder, plug *interfaces.Plug) error {
			rec.AddSnippet("permanent-plug")
			return nil
		},
		RecordTestPermanentSlotCallback: func(rec *ifacetest.TestRecorder, slot *interfaces.Slot) error {
			rec.AddSnippet("permanent-slot")
			return nil
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "test",
		},
	},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap"},
			Name:      "name",
			Interface: "test",
		},
	},
})

func (s *TestRecorderSuite) SetUpTest(c *C) {
	s.rec = &ifacetest.TestRecorder{}
}

// AddSnippet is not broken
func (s *TestRecorderSuite) TestAddSnippet(c *C) {
	s.rec.AddSnippet("hello")
	s.rec.AddSnippet("world")
	c.Assert(s.rec.Snippets, DeepEquals, []string{"hello", "world"})
}

// The TestRecorder can be used through the interfaces.Recorder interface
func (s *TestRecorderSuite) TestRecorderIface(c *C) {
	var r interfaces.Recorder = s.rec
	c.Assert(r.RecordConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.RecordConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.RecordPermanentPlug(s.iface, s.plug), IsNil)
	c.Assert(r.RecordPermanentSlot(s.iface, s.slot), IsNil)
	c.Assert(s.rec.Snippets, DeepEquals, []string{
		"connected-plug", "connected-slot", "permanent-plug", "permanent-slot"})
}
