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

package client_test

import (
	"encoding/json"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientInterfacesCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Interfaces()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientInterfaces(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"plugs": [
				{
					"snap": "canonical-pi2",
					"plug": "pin-13",
					"interface": "bool-file",
					"label": "Pin 13",
					"connections": [
						{"snap": "keyboard-lights", "slot": "capslock-led"}
					]
				}
			],
			"slots": [
				{
					"snap": "keyboard-lights",
					"slot": "capslock-led",
					"interface": "bool-file",
					"label": "Capslock indicator LED",
					"connections": [
						{"snap": "canonical-pi2", "plug": "pin-13"}
					]
				}
			]
		}
	}`
	interfaces, err := cs.cli.Interfaces()
	c.Assert(err, check.IsNil)
	c.Check(interfaces, check.DeepEquals, client.Interfaces{
		Plugs: []client.Plug{
			{
				Snap:      "canonical-pi2",
				Name:      "pin-13",
				Interface: "bool-file",
				Label:     "Pin 13",
				Connections: []client.SlotRef{
					{
						Snap: "keyboard-lights",
						Name: "capslock-led",
					},
				},
			},
		},
		Slots: []client.Slot{
			{
				Snap:      "keyboard-lights",
				Name:      "capslock-led",
				Interface: "bool-file",
				Label:     "Capslock indicator LED",
				Connections: []client.PlugRef{
					{
						Snap: "canonical-pi2",
						Name: "pin-13",
					},
				},
			},
		},
	})
}

func (cs *clientSuite) TestClientConnectCallsEndpoint(c *check.C) {
	_ = cs.cli.Connect("producer", "plug", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientConnect(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.Connect("producer", "plug", "consumer", "slot")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "connect",
		"plugs": []interface{}{
			map[string]interface{}{
				"snap": "producer",
				"plug": "plug",
			},
		},
		"slots": []interface{}{
			map[string]interface{}{
				"snap": "consumer",
				"slot": "slot",
			},
		},
	})
}

func (cs *clientSuite) TestClientDisconnectCallsEndpoint(c *check.C) {
	_ = cs.cli.Disconnect("producer", "plug", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientDisconnect(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.Disconnect("producer", "plug", "consumer", "slot")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "disconnect",
		"plugs": []interface{}{
			map[string]interface{}{
				"snap": "producer",
				"plug": "plug",
			},
		},
		"slots": []interface{}{
			map[string]interface{}{
				"snap": "consumer",
				"slot": "slot",
			},
		},
	})
}

func (cs *clientSuite) TestClientAddPlugCallsEndpoint(c *check.C) {
	_ = cs.cli.AddPlug(&client.Plug{})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientAddPlug(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.AddPlug(&client.Plug{
		Snap:      "snap",
		Name:      "plug",
		Interface: "interface",
		Attrs: map[string]interface{}{
			"attr": "value",
		},
		Apps:  []string{"app"},
		Label: "label",
	})
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "add-plug",
		"plugs": []interface{}{
			map[string]interface{}{
				"snap":      "snap",
				"plug":      "plug",
				"interface": "interface",
				"attrs": map[string]interface{}{
					"attr": "value",
				},
				"apps":  []interface{}{"app"},
				"label": "label",
			},
		},
	})
}

func (cs *clientSuite) TestClientRemovePlugCallsEndpoint(c *check.C) {
	_ = cs.cli.RemovePlug("snap", "plug")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientRemovePlug(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.RemovePlug("snap", "plug")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "remove-plug",
		"plugs": []interface{}{
			map[string]interface{}{
				"snap": "snap",
				"plug": "plug",
			},
		},
	})
}

func (cs *clientSuite) TestClientAddSlotCallsEndpoint(c *check.C) {
	_ = cs.cli.AddSlot(&client.Slot{})
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientAddSlot(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.AddSlot(&client.Slot{
		Snap:      "snap",
		Name:      "slot",
		Interface: "interface",
		Attrs: map[string]interface{}{
			"attr": "value",
		},
		Apps:  []string{"app"},
		Label: "label",
	})
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "add-slot",
		"slots": []interface{}{
			map[string]interface{}{
				"snap":      "snap",
				"slot":      "slot",
				"interface": "interface",
				"attrs": map[string]interface{}{
					"attr": "value",
				},
				"apps":  []interface{}{"app"},
				"label": "label",
			},
		},
	})
}

func (cs *clientSuite) TestClientRemoveSlotCallsEndpoint(c *check.C) {
	_ = cs.cli.RemoveSlot("snap", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/interfaces")
}

func (cs *clientSuite) TestClientRemoveSlot(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": { }
	}`
	err := cs.cli.RemoveSlot("snap", "slot")
	c.Check(err, check.IsNil)
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "remove-slot",
		"slots": []interface{}{
			map[string]interface{}{
				"snap": "snap",
				"slot": "slot",
			},
		},
	})
}
