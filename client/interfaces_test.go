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

	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientInterfacesOptionEncoding(c *check.C) {
	// Choose some options
	_, _ = cs.cli.Interfaces(&client.InterfaceQueryOptions{
		Names:     []string{"a", "b"},
		Doc:       true,
		Plugs:     true,
		Slots:     true,
		Connected: true,
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	c.Check(cs.req.URL.RawQuery, check.Equals,
		"doc=yes&names=a%2Cb&plugs=yes&select=connected&slots=yes")
}

func (cs *clientSuite) TestClientInterfacesAll(c *check.C) {
	// Ask for a summary of all interfaces.
	cs.rsp = `{
		"type": "sync",
		"result": [
			{"name": "iface-a", "summary": "the A iface"},
			{"name": "iface-b", "summary": "the B iface"},
			{"name": "iface-c", "summary": "the C iface"}
		]
	}`
	ifaces, err := cs.cli.Interfaces(nil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	// This uses the select=all query option to indicate that new response
	// format should be used. The same API endpoint is used by the Interfaces
	// and by the Connections functions an the absence or presence of the
	// select query option decides what kind of result should be returned
	// (legacy or modern).
	c.Check(cs.req.URL.RawQuery, check.Equals, "select=all")
	c.Assert(err, check.IsNil)
	c.Check(ifaces, check.DeepEquals, []*client.Interface{
		{Name: "iface-a", Summary: "the A iface"},
		{Name: "iface-b", Summary: "the B iface"},
		{Name: "iface-c", Summary: "the C iface"},
	})
}

func (cs *clientSuite) TestClientInterfacesConnected(c *check.C) {
	// Ask for for a summary of connected interfaces.
	cs.rsp = `{
		"type": "sync",
		"result": [
			{"name": "iface-a", "summary": "the A iface"},
			{"name": "iface-c", "summary": "the C iface"}
		]
	}`
	ifaces, err := cs.cli.Interfaces(&client.InterfaceQueryOptions{
		Connected: true,
	})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	// This uses select=connected to ignore interfaces that just sit on some
	// snap but are not connected to anything.
	c.Check(cs.req.URL.RawQuery, check.Equals, "select=connected")
	c.Assert(err, check.IsNil)
	c.Check(ifaces, check.DeepEquals, []*client.Interface{
		{Name: "iface-a", Summary: "the A iface"},
		// interface b was not connected so it doesn't get listed.
		{Name: "iface-c", Summary: "the C iface"},
	})
}

func (cs *clientSuite) TestClientInterfacesSelectedDetails(c *check.C) {
	// Ask for single element and request docs, plugs and slots.
	cs.rsp = `{
		"type": "sync",
		"result": [
			{
				"name": "iface-a",
				"summary": "the A iface",
				"doc-url": "http://example.org/ifaces/a",
				"plugs": [{
					"snap": "consumer",
					"plug": "plug",
					"interface": "iface-a"
				}],
				"slots": [{
					"snap": "producer",
					"slot": "slot",
					"interface": "iface-a"
				}]
			}
		]
	}`
	opts := &client.InterfaceQueryOptions{Names: []string{"iface-a"}, Doc: true, Plugs: true, Slots: true}
	ifaces, err := cs.cli.Interfaces(opts)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	// This enables documentation, plugs, slots, chooses a specific interface
	// (iface-a), and uses select=all to indicate that new response is desired.
	c.Check(cs.req.URL.RawQuery, check.Equals,
		"doc=yes&names=iface-a&plugs=yes&select=all&slots=yes")
	c.Assert(err, check.IsNil)
	c.Check(ifaces, check.DeepEquals, []*client.Interface{
		{
			Name:    "iface-a",
			Summary: "the A iface",
			DocURL:  "http://example.org/ifaces/a",
			Plugs:   []client.Plug{{Snap: "consumer", Name: "plug", Interface: "iface-a"}},
			Slots:   []client.Slot{{Snap: "producer", Name: "slot", Interface: "iface-a"}},
		},
	})
}

func (cs *clientSuite) TestClientInterfacesMultiple(c *check.C) {
	// Ask for multiple interfaces.
	cs.rsp = `{
		"type": "sync",
		"result": [
			{"name": "iface-a", "summary": "the A iface"},
			{"name": "iface-b", "summary": "the B iface"}
		]
	}`
	ifaces, err := cs.cli.Interfaces(&client.InterfaceQueryOptions{Names: []string{"iface-a", "iface-b"}})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	// This chooses a specific interfaces (iface-a, iface-b)
	c.Check(cs.req.URL.RawQuery, check.Equals, "names=iface-a%2Ciface-b&select=all")
	c.Assert(err, check.IsNil)
	c.Check(ifaces, check.DeepEquals, []*client.Interface{
		{Name: "iface-a", Summary: "the A iface"},
		{Name: "iface-b", Summary: "the B iface"},
	})
}

func (cs *clientSuite) TestClientConnectionsCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Connections()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
}

func (cs *clientSuite) TestClientConnections(c *check.C) {
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
	interfaces, err := cs.cli.Connections()
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

func (cs *clientSuite) TestClientInterfaceIndex(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [
			{"name": "iface-1", "summary": "summary", "used": true },
			{"name": "iface-2", "summary": "summary"}
		]
	}`
	names, err := cs.cli.InterfaceIndex()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interface")
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []client.Interface{
		{Name: "iface-1", Summary: "summary", Used: true},
		{Name: "iface-2", Summary: "summary"},
	})
}

func (cs *clientSuite) TestClientInterface(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"name": "bool-file",
			"summary": "The bool-file interface allows access to a specific file that contains values 0 or 1",
			"doc-url": "http://example.org/",
			"plugs": [
				{
					"snap": "canonical-pi2",
					"plug": "pin-13",
					"label": "Pin 13"
				}
			],
			"slots": [
				{
					"snap": "keyboard-lights",
					"slot": "capslock-led",
					"label": "Capslock indicator LED"
				}
			]
		}
	}`
	iface, err := cs.cli.Interface("bool-file")
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interface/bool-file")
	c.Check(iface, check.DeepEquals, client.Interface{
		Name:    "bool-file",
		Summary: "The bool-file interface allows access to a specific file that contains values 0 or 1",
		DocURL:  "http://example.org/",
		Plugs: []client.Plug{
			{
				Snap:  "canonical-pi2",
				Name:  "pin-13",
				Label: "Pin 13",
			},
		},
		Slots: []client.Slot{
			{
				Snap:  "keyboard-lights",
				Name:  "capslock-led",
				Label: "Capslock indicator LED",
			},
		},
	})
}

func (cs *clientSuite) TestClientConnectCallsEndpoint(c *check.C) {
	cs.cli.Connect("producer", "plug", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
}

func (cs *clientSuite) TestClientConnect(c *check.C) {
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "foo"
	}`
	id, err := cs.cli.Connect("producer", "plug", "consumer", "slot")
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "foo")
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
	cs.cli.Disconnect("producer", "plug", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
}

func (cs *clientSuite) TestClientDisconnect(c *check.C) {
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "42"
	}`
	id, err := cs.cli.Disconnect("producer", "plug", "consumer", "slot")
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "42")
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
