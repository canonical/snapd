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

package interfaces_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	. "github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

type JSONSuite struct {
	plug *Plug
	slot *Slot
}

var _ = Suite(&JSONSuite{
	plug: &Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "snap-name"},
			Name:      "plug-name",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps: map[string]*snap.AppInfo{
				"app-name": {
					Name: "app-name",
				},
			},
			Label: "label",
		},
		Connections: []SlotRef{{
			Snap: "other-snap-name",
			Name: "slot-name",
		}},
	},
	slot: &Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "snap-name"},
			Name:      "slot-name",
			Interface: "interface",
			Attrs:     map[string]interface{}{"key": "value"},
			Apps: map[string]*snap.AppInfo{
				"app-name": {
					Name: "app-name",
				},
			},
			Label: "label",
		},
		Connections: []PlugRef{{
			Snap: "other-snap-name",
			Name: "plug-name",
		}},
	},
})

func (s *JSONSuite) TestPlugMarshalJSON(c *C) {
	data, err := json.Marshal(s.plug)
	c.Assert(err, IsNil)
	var repr map[string]interface{}
	err = json.Unmarshal(data, &repr)
	c.Assert(err, IsNil)
	c.Check(repr, DeepEquals, map[string]interface{}{
		"snap":      "snap-name",
		"plug":      "plug-name",
		"interface": "interface",
		"attrs":     map[string]interface{}{"key": "value"},
		"apps":      []interface{}{"app-name"},
		"label":     "label",
		"connections": []interface{}{
			map[string]interface{}{"snap": "other-snap-name", "slot": "slot-name"},
		},
	})
}

func (s *JSONSuite) TestSlotMarshalJSON(c *C) {
	data, err := json.Marshal(s.slot)
	c.Assert(err, IsNil)
	var repr map[string]interface{}
	err = json.Unmarshal(data, &repr)
	c.Assert(err, IsNil)
	c.Check(repr, DeepEquals, map[string]interface{}{
		"snap":      "snap-name",
		"slot":      "slot-name",
		"interface": "interface",
		"attrs":     map[string]interface{}{"key": "value"},
		"apps":      []interface{}{"app-name"},
		"label":     "label",
		"connections": []interface{}{
			map[string]interface{}{"snap": "other-snap-name", "plug": "plug-name"},
		},
	})
}

func (s *JSONSuite) TestInterfaceInfoMarshalJSON(c *C) {
	md := MetaData{
		Summary: "interface summary",
		DocsURL: "http://example.org/",
	}
	ifaceInfo := &InterfaceInfo{
		Name:     "iface",
		MetaData: md,
		Plugs:    []*snap.PlugInfo{s.plug.PlugInfo},
		Slots:    []*snap.SlotInfo{s.slot.SlotInfo},
	}
	data, err := json.Marshal(ifaceInfo)
	c.Assert(err, IsNil)
	var repr map[string]interface{}
	err = json.Unmarshal(data, &repr)
	c.Assert(err, IsNil)
	c.Check(repr, DeepEquals, map[string]interface{}{
		"name":     "iface",
		"summary":  "interface summary",
		"docs-url": "http://example.org/",
		"plugs": []interface{}{
			map[string]interface{}{
				"snap":  "snap-name",
				"plug":  "plug-name",
				"attrs": map[string]interface{}{"key": "value"},
				"label": "label",
			},
		},
		"slots": []interface{}{
			map[string]interface{}{
				"snap":  "snap-name",
				"slot":  "slot-name",
				"attrs": map[string]interface{}{"key": "value"},
				"label": "label",
			},
		},
	})
}
