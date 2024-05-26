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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientInterfacesOptionEncoding(c *check.C) {
	// Choose some options
	_, _ = cs.cli.Interfaces(&client.InterfaceOptions{
		Names:     []string{"a", "b"},
		Doc:       true,
		Plugs:     true,
		Slots:     true,
		Connected: true,
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	c.Check(cs.req.URL.RawQuery, check.Equals,
		"doc=true&names=a%2Cb&plugs=true&select=connected&slots=true")
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
	ifaces := mylog.Check2(cs.cli.Interfaces(nil))
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
	// Ask for a summary of connected interfaces.
	cs.rsp = `{
		"type": "sync",
		"result": [
			{"name": "iface-a", "summary": "the A iface"},
			{"name": "iface-c", "summary": "the C iface"}
		]
	}`
	ifaces := mylog.Check2(cs.cli.Interfaces(&client.InterfaceOptions{
		Connected: true,
	}))
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
	opts := &client.InterfaceOptions{Names: []string{"iface-a"}, Doc: true, Plugs: true, Slots: true}
	ifaces := mylog.Check2(cs.cli.Interfaces(opts))
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
	// This enables documentation, plugs, slots, chooses a specific interface
	// (iface-a), and uses select=all to indicate that new response is desired.
	c.Check(cs.req.URL.RawQuery, check.Equals,
		"doc=true&names=iface-a&plugs=true&select=all&slots=true")
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
	ifaces := mylog.Check2(cs.cli.Interfaces(&client.InterfaceOptions{Names: []string{"iface-a", "iface-b"}}))
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

func (cs *clientSuite) TestClientConnectCallsEndpoint(c *check.C) {
	cs.cli.Connect("producer", "plug", "consumer", "slot")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
}

func (cs *clientSuite) TestClientConnect(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "foo"
	}`
	id := mylog.Check2(cs.cli.Connect("producer", "plug", "consumer", "slot"))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "foo")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
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
	cs.cli.Disconnect("producer", "plug", "consumer", "slot", nil)
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/interfaces")
}

func (cs *clientSuite) TestClientDisconnect(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "42"
	}`
	opts := &client.DisconnectOptions{Forget: false}
	id := mylog.Check2(cs.cli.Disconnect("producer", "plug", "consumer", "slot", opts))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "42")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
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

func (cs *clientSuite) TestClientDisconnectForget(c *check.C) {
	cs.status = 202
	cs.rsp = `{
		"type": "async",
                "status-code": 202,
		"result": { },
                "change": "42"
	}`
	opts := &client.DisconnectOptions{Forget: true}
	id := mylog.Check2(cs.cli.Disconnect("producer", "plug", "consumer", "slot", opts))
	c.Assert(err, check.IsNil)
	c.Check(id, check.Equals, "42")
	var body map[string]interface{}
	decoder := json.NewDecoder(cs.req.Body)
	mylog.Check(decoder.Decode(&body))
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"action": "disconnect",
		"forget": true,
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
