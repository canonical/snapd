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

package ifacestate_test

import (
	"encoding/json"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
)

type intentSuite struct {
	intent      ifacestate.Intent
	otherIntent ifacestate.Intent
}

var _ = check.Suite(&intentSuite{
	intent: ifacestate.Intent{
		Plug: interfaces.PlugRef{"snap", "plug"},
		Slot: interfaces.SlotRef{"snap", "slot"},
	},
	otherIntent: ifacestate.Intent{
		Plug: interfaces.PlugRef{"other-snap", "plug"},
		Slot: interfaces.SlotRef{"other-snap", "slot"},
	},
})

func (s *intentSuite) TestMarshallToJSON(c *check.C) {
	data, err := json.Marshal(s.intent)
	c.Assert(err, check.IsNil)
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	c.Assert(err, check.IsNil)
	c.Check(result, check.DeepEquals, map[string]interface{}{
		"plug": map[string]interface{}{
			"snap": "snap",
			"plug": "plug",
		},
		"slot": map[string]interface{}{
			"snap": "snap",
			"slot": "slot",
		},
	})
}

func (s *intentSuite) TestMarshallFromJSON(c *check.C) {
	data := []byte(`
	{
		"plug": { 
			"snap": "snap",
			"plug": "plug"
		},
		"slot": { 
			"snap": "snap",
			"slot": "slot"
		}
	}`)
	var intent ifacestate.Intent
	err := json.Unmarshal(data, &intent)
	c.Assert(err, check.IsNil)
	c.Check(intent, check.DeepEquals, s.intent)
}

func (s *intentSuite) TestAdd(c *check.C) {
	var intents ifacestate.Intents
	intents.Add(s.intent)
	c.Assert(intents, check.DeepEquals, ifacestate.Intents{s.intent})
	// Adding another one won't do anything because it is the same
	intents.Add(s.intent)
	c.Assert(intents, check.DeepEquals, ifacestate.Intents{s.intent})
	// Adding an unrelated one will just append it
	intents.Add(s.otherIntent)
	c.Assert(intents, check.DeepEquals, ifacestate.Intents{s.intent, s.otherIntent})
}

func (s *intentSuite) TestRemove(c *check.C) {
	intents := ifacestate.Intents{s.intent}
	// Removing intents that are not present is harmless
	intents.Remove(s.otherIntent)
	c.Assert(intents, check.HasLen, 1)
	// Removing intents that are present works
	intents.Remove(s.intent)
	c.Assert(intents, check.HasLen, 0)
}
