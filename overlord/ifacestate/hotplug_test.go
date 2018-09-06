// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate_test

import (
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/overlord/ifacestate"

	. "gopkg.in/check.v1"
)

type hotplugSuite struct{}

var _ = Suite(&hotplugSuite{})

func (s *hotplugSuite) TestEnsureUniqueName(c *C) {
	fakeRepositoryLookup := func(n string) bool {
		reserved := map[string]bool{
			"slot1":    true,
			"slot":     true,
			"slot1234": true,
			"slot-1":   true,
			"slot-2":   true,
			"slot3-5":  true,
			"slot3-6":  true,
			"11":       true,
			"12foo":    true,
		}
		return !reserved[n]
	}

	names := []struct{ proposedName, resultingName string }{
		{"foo", "foo"},
		{"slot1", "slot2"},
		{"slot1234", "slot1235"},
		{"slot-1", "slot-3"},
		{"slot3-5", "slot3-7"},
		{"slot3-1", "slot3-1"},
		{"11", "12"},
		{"12foo", "12foo-1"},
	}

	for _, name := range names {
		c.Assert(ifacestate.EnsureUniqueName(name.proposedName, fakeRepositoryLookup), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestCleanupSlotName(c *C) {
	names := []struct{ proposedName, resultingName string }{
		{"", ""},
		{"-", ""},
		{"slot1", "slot1"},
		{"-slot1", "slot1"},
		{"a--slot-1", "a-slot-1"},
		{"(-slot", "slot"},
		{"(--slot", "slot"},
		{"slot-", "slot"},
		{"slot---", "slot"},
		{"slot-(", "slot"},
		{"Integrated_Webcam_HD", "integratedwebcamhd"},
		{"Xeon E3-1200 v5/E3-1500 v5/6th Gen Core Processor Host Bridge/DRAM Registers", "xeone3-1200v5e3-1500v5"},
	}
	for _, name := range names {
		c.Assert(ifacestate.CleanupSlotName(name.proposedName), Equals, name.resultingName)
	}
}

func (s *hotplugSuite) TestSuggestedSlotName(c *C) {

	events := []struct {
		eventData map[string]string
		outName   string
	}{{
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"NAME":                   "Name",
			"ID_MODEL":               "Longer Name",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"name",
	}, {
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"ID_MODEL":               "Longer Name",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"longername",
	}, {
		map[string]string{
			"DEVPATH":                "a/path",
			"ACTION":                 "add",
			"SUBSYSTEM":              "foo",
			"ID_MODEL_FROM_DATABASE": "Longest Name",
		},
		"longestname",
	}, {
		map[string]string{
			"DEVPATH":   "a/path",
			"ACTION":    "add",
			"SUBSYSTEM": "foo",
		},
		"fallbackname",
	},
	}

	for _, data := range events {
		di, err := hotplug.NewHotplugDeviceInfo(data.eventData)
		c.Assert(err, IsNil)

		slotName := ifacestate.SuggestedSlotName(di, "fallbackname")
		c.Assert(slotName, Equals, data.outName)
	}
}
